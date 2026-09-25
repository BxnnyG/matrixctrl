package restore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

type fakePod struct {
	steps   []string
	files   []string // what `find` reports after the tar
	free    int64
	written string
	failOn  map[string]error
}

func (f *fakePod) RunInPod(_ context.Context, _, _, _ string, cmd ...string) (string, error) {
	f.steps = append(f.steps, strings.Join(cmd, " "))
	if err := f.failOn[cmd[0]]; err != nil {
		return "", err
	}
	if cmd[0] == "find" {
		return strings.Join(f.files, "\n"), nil
	}
	return "", nil
}

func (f *fakePod) TarIntoPod(_ context.Context, _, _, _, dir string, in io.Reader) error {
	body, _ := io.ReadAll(in)
	f.written = string(body)
	f.steps = append(f.steps, "tar -> "+dir)
	return f.failOn["tar"]
}

func (f *fakePod) FreeSpaceInPod(context.Context, string, string, string, string) (int64, error) {
	return f.free, nil
}

func mediaPod() *fakePod {
	return &fakePod{
		files:  []string{"/media/.matrixctrl-restore-s/media_store/local_content/aa/bb/cc"},
		free:   10 << 30,
		failOn: map[string]error{},
	}
}

// The order matters: stage, unpack, count, and only then put into place.
func TestMediaStagesBeforeItPlaces(t *testing.T) {
	p := mediaPod()

	n, err := Media(context.Background(), p, "ess", "pod", "synapse", "/media", "s",
		1<<20, strings.NewReader("tarbytes"), nil)
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	if n != 1 {
		t.Errorf("one file was reported by find, got %d", n)
	}

	want := []string{
		"rm -rf /media/.matrixctrl-restore-s",
		"mkdir -p /media/.matrixctrl-restore-s",
		"tar -> /media/.matrixctrl-restore-s",
		"find /media/.matrixctrl-restore-s -type f",
		"cp -a /media/.matrixctrl-restore-s/. /media",
		"rm -rf /media/.matrixctrl-restore-s",
	}
	if got := strings.Join(p.steps, " | "); got != strings.Join(want, " | ") {
		t.Errorf("\n got: %s\nwant: %s", got, strings.Join(want, " | "))
	}
	if p.written != "tarbytes" {
		t.Errorf("the archive's bytes must reach the pod unchanged: %q", p.written)
	}
}

// tar exits 0 on an empty stream. Without a count that is indistinguishable from a
// successful restore of nothing — and nothing is what the operator would then have.
func TestMediaRefusesAnEmptyStream(t *testing.T) {
	p := mediaPod()
	p.files = nil

	_, err := Media(context.Background(), p, "ess", "pod", "synapse", "/media", "s",
		0, strings.NewReader(""), nil)
	if err == nil {
		t.Fatal("an empty unpack must not be reported as a restore")
	}
	if strings.Contains(strings.Join(p.steps, " "), "cp -a") {
		t.Error("nothing may be copied into place when nothing arrived")
	}
	if !strings.Contains(strings.Join(p.steps, " | "), "rm -rf") {
		t.Error("the staging directory must be cleaned up even on failure")
	}
}

// Running out of room halfway through leaves half the uploads in a staging directory.
// The number exists beforehand, so it is a sentence rather than a surprise.
func TestMediaRefusesWhenThereIsNotEnoughRoom(t *testing.T) {
	p := mediaPod()
	p.free = 100 << 20 // 100 MB free

	_, err := Media(context.Background(), p, "ess", "pod", "synapse", "/media", "s",
		80<<20, strings.NewReader("x"), nil) // 80 MB of files needs 160 MB
	if err == nil {
		t.Fatal("a restore that cannot fit must be refused before it writes")
	}
	if len(p.steps) != 0 {
		t.Errorf("nothing may happen before the space check: %v", p.steps)
	}
	for _, want := range []string{"frei", "doppelt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message must explain why twice the size is needed: %v", err)
		}
	}
}

func TestMediaCleansUpAfterAFailedUnpack(t *testing.T) {
	p := mediaPod()
	p.failOn["tar"] = errors.New("stream closed")

	if _, err := Media(context.Background(), p, "ess", "pod", "synapse", "/media", "s",
		0, strings.NewReader("x"), nil); err == nil {
		t.Fatal("expected failure")
	}
	last := p.steps[len(p.steps)-1]
	if last != "rm -rf /media/.matrixctrl-restore-s" {
		t.Errorf("the staging directory must be removed last, got %q", last)
	}
}

type fakeSecrets struct {
	data map[string][]byte
	put  map[string][]byte
}

func (f *fakeSecrets) GetSecret(context.Context, string, string) (map[string][]byte, error) {
	return f.data, nil
}

func (f *fakeSecrets) PutSecret(_ context.Context, _, _ string, data map[string][]byte) error {
	f.put = data
	return nil
}

// The trap: ess-generated holds the homeserver's identity *and* the passwords of this
// installation's Postgres roles. Restoring it wholesale writes the source's passwords
// into the secret while the target's database still expects its own — every step reports
// success and nothing starts.
func TestKeysRestoreIdentityAndKeepLocalCredentials(t *testing.T) {
	key, _, err := backup.NewRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := backup.Seal(key, []byte(`
apiVersion: v1
kind: Secret
metadata:
  name: ess-generated
data:
  SYNAPSE_SIGNING_KEY: b2xk
  SYNAPSE_MACAROON: b2xk
  MAS_ENCRYPTION_SECRET: b2xk
  POSTGRES_SYNAPSE_PASSWORD: ZnJvbS10aGUtb2xkLXNlcnZlcg==
`))
	if err != nil {
		t.Fatal(err)
	}

	here := &fakeSecrets{data: map[string][]byte{
		"SYNAPSE_SIGNING_KEY":       []byte("new"),
		"POSTGRES_SYNAPSE_PASSWORD": []byte("belongs-to-this-cluster"),
		"POSTGRES_ADMIN_PASSWORD":   []byte("belongs-to-this-cluster"),
	}}

	res, err := Keys(context.Background(), here, "ess", sealed, key, nil)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if res.Name != "ess-generated" {
		t.Errorf("the secret's own name must be used: %q", res.Name)
	}
	if got := string(here.put["SYNAPSE_SIGNING_KEY"]); got != "old" {
		t.Errorf("the identity must come from the archive, got %q", got)
	}
	if got := string(here.put["POSTGRES_SYNAPSE_PASSWORD"]); got != "belongs-to-this-cluster" {
		t.Errorf("the database password of this installation must survive, got %q", got)
	}
	if got := string(here.put["POSTGRES_ADMIN_PASSWORD"]); got != "belongs-to-this-cluster" {
		t.Errorf("an entry the archive does not carry must survive, got %q", got)
	}
	if len(res.Kept) != 2 {
		t.Errorf("the operator must be told what was kept: %v", res.Kept)
	}
	if len(res.Restored) != 3 {
		t.Errorf("three identity keys must be reported as restored: %v", res.Restored)
	}
}

// A wrong recovery key must say so in a way that explains where the right one is, and
// must not write anything.
func TestKeysRefuseTheWrongRecoveryKey(t *testing.T) {
	key, _, _ := backup.NewRecoveryKey()
	other, _, _ := backup.NewRecoveryKey()
	sealed, err := backup.Seal(key, []byte("kind: Secret\nmetadata:\n  name: x\n"))
	if err != nil {
		t.Fatal(err)
	}

	here := &fakeSecrets{data: map[string][]byte{}}
	if _, err := Keys(context.Background(), here, "ess", sealed, other, nil); err == nil {
		t.Fatal("the wrong key must be refused")
	} else if !strings.Contains(err.Error(), "Wiederherstellungsschlüssel") {
		t.Errorf("the message must name what is needed: %v", err)
	}
	if here.put != nil {
		t.Error("nothing may be written when the archive could not be opened")
	}
}

func TestHumanReadsAsAnOperatorWouldSayIt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{fmt.Sprint(1 << 30), "1.0 GB"},
		{fmt.Sprint(40 << 20), "40 MB"},
	} {
		var n int64
		fmt.Sscan(tc.in, &n)
		if got := human(n); got != tc.want {
			t.Errorf("human(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
