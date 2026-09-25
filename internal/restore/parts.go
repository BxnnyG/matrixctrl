package restore

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// The two parts that are not databases: the uploaded files, and the keys.

// Pod is what a media restore needs from the cluster.
type Pod interface {
	RunInPod(ctx context.Context, namespace, pod, container string, cmd ...string) (string, error)
	TarIntoPod(ctx context.Context, namespace, pod, container, dir string, in io.Reader) error
	FreeSpaceInPod(ctx context.Context, namespace, pod, container, dir string) (int64, error)
}

// Media unpacks the uploaded files back onto the volume only Synapse mounts.
//
// It stages first. /media is a mount point: nothing can be created beside it and it
// cannot be renamed, so "unpack next to it and swap" — the shape that broke the config
// restore in etappe 73 — is not available. The staging directory therefore lives
// *inside* the volume, and only a tar that unpacked completely is moved into place.
//
// Moving into place is a copy rather than a swap, and that is correct here rather than
// a compromise: Synapse's media store is content-addressed, so a file that exists under
// the same path holds the same bytes. Restoring is additive, and a file the target has
// that the archive does not is left alone — which is what an operator restoring
// yesterday's archive onto today's server wants.
func Media(ctx context.Context, p Pod, ns, pod, container, dir, stamp string,
	size int64, in io.Reader, prog Progress) (int64, error) {

	staging := dir + "/.matrixctrl-restore-" + stamp

	if size > 0 {
		free, err := p.FreeSpaceInPod(ctx, ns, pod, container, dir)
		if err != nil {
			return 0, err
		}
		// Twice, because the files exist in the staging directory and in their final
		// place at the same time. Asked before anything is written: running out of room
		// halfway through leaves an operator with a staging directory and no message.
		if free < size*2 {
			return 0, fmt.Errorf("auf %s sind %s frei, für die Dateien werden %s gebraucht "+
				"(sie liegen beim Einspielen kurz doppelt da)",
				dir, human(free), human(size*2))
		}
		prog.say("media", "%s frei auf %s, %s werden gebraucht", human(free), dir, human(size*2))
	}

	if _, err := p.RunInPod(ctx, ns, pod, container, "rm", "-rf", staging); err != nil {
		return 0, fmt.Errorf("Reste eines früheren Laufs entfernen: %w", err)
	}
	if _, err := p.RunInPod(ctx, ns, pod, container, "mkdir", "-p", staging); err != nil {
		return 0, err
	}
	// Always cleaned up: a staging directory left inside the media volume is counted by
	// every size check afterwards and looks like the uploads doubling overnight.
	defer func() {
		_, _ = p.RunInPod(context.WithoutCancel(ctx), ns, pod, container, "rm", "-rf", staging)
	}()

	prog.say("media", "die Dateien werden in den Pod gestreamt")
	if err := p.TarIntoPod(ctx, ns, pod, container, staging, in); err != nil {
		return 0, err
	}

	// tar exits 0 on an empty stream, which is indistinguishable from a successful
	// restore of nothing unless somebody counts.
	out, err := p.RunInPod(ctx, ns, pod, container, "find", staging, "-type", "f")
	if err != nil {
		return 0, err
	}
	files := int64(len(strings.Fields(out)))
	if files == 0 {
		return 0, fmt.Errorf("es sind keine Dateien angekommen — das Archiv enthielt keine, " +
			"oder der Strom ist abgebrochen")
	}
	prog.say("media", "%d Dateien angekommen", files)

	if _, err := p.RunInPod(ctx, ns, pod, container, "cp", "-a", staging+"/.", dir); err != nil {
		return files, fmt.Errorf("die Dateien an ihren Platz kopieren: %w", err)
	}
	prog.say("media", "%d Dateien liegen wieder in %s", files, dir)
	return files, nil
}

// KeysKeptLocal are the entries of ess-generated that belong to *this* installation and
// must survive a restore of somebody else's — including this server's own past self on
// another cluster.
//
// They are the database passwords. Postgres roles are created when the chart is
// installed, and their passwords live in the running server's `pg_authid`, not in the
// archive. Writing the source's passwords into the secret would leave Synapse holding a
// password its own database has never heard of: every part of the restore would report
// success and nothing would start.
//
// Everything else in that secret is the homeserver's *identity* — the signing key that
// makes federation believe it is the same server, the macaroon key that keeps existing
// sessions valid, the MAS encryption secret without which the account database cannot be
// decrypted — and that is the entire reason the keys are in the archive.
var KeysKeptLocal = map[string]bool{
	"POSTGRES_ADMIN_PASSWORD":                         true,
	"POSTGRES_SYNAPSE_PASSWORD":                       true,
	"POSTGRES_MATRIX_AUTHENTICATION_SERVICE_PASSWORD": true,
}

// Secrets is what a key restore needs from the cluster.
type Secrets interface {
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
	PutSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
}

// KeyResult says exactly which entries moved and which stayed, because "the keys were
// restored" is not a sentence anyone can check.
type KeyResult struct {
	Name     string   `json:"name"`
	Restored []string `json:"restored"`
	Kept     []string `json:"kept"`
}

// Keys puts the homeserver's identity back, without taking the installation's plumbing
// with it.
//
// The sealed part is opened with the recovery key the operator was shown once when the
// archive was made. Losing it costs the sessions and nothing else: rooms, messages,
// accounts and files all restore without it (§4.102), which is why only this part is
// encrypted.
func Keys(ctx context.Context, s Secrets, ns string, sealed, recoveryKey []byte, prog Progress) (KeyResult, error) {
	var res KeyResult

	plain, err := backup.Open(recoveryKey, sealed)
	if err != nil {
		return res, fmt.Errorf("die Schlüssel lassen sich mit diesem Wiederherstellungsschlüssel " +
			"nicht öffnen — er gehört zu genau diesem Archiv und wurde beim Herunterladen einmal angezeigt")
	}

	var sec corev1.Secret
	if err := yaml.Unmarshal(plain, &sec); err != nil {
		return res, fmt.Errorf("der entschlüsselte Teil ist kein Secret: %w", err)
	}
	if sec.Name == "" {
		return res, fmt.Errorf("der entschlüsselte Teil hat keinen Namen")
	}
	res.Name = sec.Name

	current, err := s.GetSecret(ctx, ns, sec.Name)
	if err != nil {
		return res, err
	}

	merged := map[string][]byte{}
	for k, v := range sec.Data {
		if KeysKeptLocal[k] {
			continue
		}
		merged[k] = v
		res.Restored = append(res.Restored, k)
	}
	for k, v := range current {
		if _, taken := merged[k]; taken {
			continue
		}
		merged[k] = v
		if KeysKeptLocal[k] {
			res.Kept = append(res.Kept, k)
		}
	}

	if err := s.PutSecret(ctx, ns, sec.Name, merged); err != nil {
		return res, err
	}
	prog.say("keys", "%d Schlüssel zurückgespielt, %d Zugangsdaten dieser Installation behalten",
		len(res.Restored), len(res.Kept))
	return res, nil
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", b/1024)
	}
}
