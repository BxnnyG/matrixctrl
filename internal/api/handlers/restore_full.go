package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup"
	"github.com/bxnnyg/matrixctrl/internal/restore"
)

// Restoring every part of an archive, over the web interface (etappe 106).
//
// "es soll aber über die webui funktionieren like apple user like unifi user like daus."
// Until now the answer was a sentence in the archive saying it had to be done with psql.
//
// Two shapes decide this file:
//
//   - **It does not answer while it works.** A restore stops Synapse, waits for it to
//     build a schema and loads a database; that is minutes, and the proxy in front of
//     this panel gives up after about a hundred seconds. So the upload starts a job and
//     returns, and the progress is polled. The upgrade stream solves the same problem
//     with a WebSocket, which is the right tool for a thousand log lines a minute and
//     the wrong one for fifty steps.
//
//   - **The archive is spooled to disk, never held.** An archive with media is as large
//     as the homeserver's uploads. It is written to a temporary file and walked several
//     times from there.
type restoreJob struct {
	mu      sync.Mutex
	running bool
	state   restoreState
}

// restoreState is what a reader is given: the job's contents without its lock.
//
// Separate from restoreJob because a snapshot is copied out, and copying a struct that
// contains a mutex copies the mutex — harmless here by luck and wrong in general, which
// is why `go vet` refuses it. Splitting the state from the thing that guards it says
// which half may leave the lock.
type restoreState struct {
	Started time.Time      `json:"started"`
	Steps   []restoreStep  `json:"steps"`
	Done    bool           `json:"done"`
	Failed  string         `json:"failed,omitempty"`
	Summary *restoreReport `json:"summary,omitempty"`
	Running bool           `json:"running"`
}

type restoreStep struct {
	At     time.Time `json:"at"`
	Step   string    `json:"step"`
	Detail string    `json:"detail"`
}

type restoreReport struct {
	ConfigFiles   int                `json:"config_files"`
	OwnTables     []string           `json:"own_tables"`
	Databases     []restore.Result   `json:"databases"`
	MediaFiles    int64              `json:"media_files"`
	Keys          *restore.KeyResult `json:"keys,omitempty"`
	Omitted       []string           `json:"omitted"`
	RestartNeeded bool               `json:"restart_needed"`
}

func (j *restoreJob) say(step, detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state.Steps = append(j.state.Steps, restoreStep{At: time.Now().UTC(), Step: step, Detail: detail})
	log.Printf("restore: %s — %s", step, detail)
}

func (j *restoreJob) finish(summary *restoreReport, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state.Done, j.running, j.state.Running, j.state.Summary = true, false, false, summary
	if err != nil {
		j.state.Failed = err.Error()
	}
}

// snapshot copies the job under the lock so the handler never encodes a struct that is
// being written to.
func (j *restoreJob) snapshot() restoreState {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.state
	out.Steps = append([]restoreStep(nil), j.state.Steps...)
	return out
}

// POST /api/v1/status/restore/full — take the archive, start the work, answer at once.
func (h *StatusHandler) RestoreFull(w http.ResponseWriter, r *http.Request) {
	if h.backupDB == nil {
		Error(w, http.StatusServiceUnavailable, "Für diese Installation ist keine Wiederherstellung verfügbar.")
		return
	}
	h.restoreMu.Lock()
	if h.restore != nil && h.restore.running {
		h.restoreMu.Unlock()
		Error(w, http.StatusConflict, "Es läuft bereits eine Wiederherstellung.")
		return
	}
	job := &restoreJob{running: true, state: restoreState{Started: time.Now().UTC(), Running: true}}
	h.restore = job
	h.restoreMu.Unlock()

	// Spooled before anything else: the upload is the one part that fails often (a
	// dropped connection, a proxy limit), and failing before a single container is
	// stopped is the difference between "try again" and "my server is down".
	spool, err := os.CreateTemp("", "mxctrl-restore-*.tar.gz")
	if err != nil {
		job.finish(nil, err)
		Error(w, http.StatusInternalServerError, "Kein Platz für das Archiv: "+err.Error())
		return
	}
	size, err := io.Copy(spool, r.Body)
	if err != nil {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
		job.finish(nil, err)
		Error(w, http.StatusBadRequest, "Das Archiv kam nicht vollständig an: "+err.Error())
		return
	}
	_ = spool.Close()
	job.say("upload", fmt.Sprintf("%.1f MB empfangen", float64(size)/1024/1024))

	opts := restoreOptions{
		Media:       r.URL.Query().Get("media") != "false",
		Keys:        r.URL.Query().Get("keys") != "false",
		RecoveryKey: r.Header.Get("X-MatrixCtrl-Recovery-Key"),
	}

	// Detached from the request: the operator's browser may close, the restore must not.
	go func() {
		defer os.Remove(spool.Name())
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Hour)
		defer cancel()
		report, err := h.runRestore(ctx, spool.Name(), opts, job)
		job.finish(report, err)
		if err != nil {
			log.Printf("restore: failed: %v", err)
		}
	}()

	JSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// GET /api/v1/status/restore/progress — what has happened so far.
func (h *StatusHandler) RestoreProgress(w http.ResponseWriter, r *http.Request) {
	h.restoreMu.Lock()
	job := h.restore
	h.restoreMu.Unlock()
	if job == nil {
		JSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}
	JSON(w, http.StatusOK, job.snapshot())
}

type restoreOptions struct {
	Media       bool
	Keys        bool
	RecoveryKey string
}

// runRestore is the order of the whole operation, and the order is the design.
//
// MatrixCtrl's own part first: it is small, it is reversible, and if the archive is not
// what it claims to be this is where that shows — before anything in ESS is stopped.
// Then the databases, each one swapped rather than overwritten. Then the media, which
// needs Synapse running again, which it is by then. The keys last, because they are the
// step that changes who the server *is*.
func (h *StatusHandler) runRestore(ctx context.Context, path string, opts restoreOptions, job *restoreJob) (*restoreReport, error) {
	report := &restoreReport{RestartNeeded: true}
	stamp := restore.Stamp(time.Now())

	full, parts, err := readFullManifest(path)
	if err != nil {
		return report, err
	}
	job.say("archive", fmt.Sprintf("Archivformat %d, erstellt %s, Teile: %s",
		full.FormatVersion, full.CreatedAt.Format("2006-01-02 15:04"), strings.Join(full.Parts, " ")))
	report.Omitted = full.NotIncluded

	// MatrixCtrl's own configuration and database, through the path that has existed
	// since etappe 69 — opened from the spool rather than from memory.
	f, err := os.Open(path)
	if err != nil {
		return report, err
	}
	a, err := backup.Read(f)
	_ = f.Close()
	if err != nil {
		return report, err
	}
	files, err := a.RestoreConfigRepo(h.backupRepo)
	if err != nil {
		return report, fmt.Errorf("die Konfiguration konnte nicht wiederhergestellt werden: %w", err)
	}
	report.ConfigFiles = files
	job.say("config", fmt.Sprintf("%d Konfigurationsdateien zurückgespielt", files))

	tables, err := a.RestoreDatabase(ctx, h.backupDB)
	if err != nil {
		return report, fmt.Errorf("MatrixCtrls eigene Datenbank: %w", err)
	}
	report.OwnTables = tables
	job.say("matrixctrl", fmt.Sprintf("%d eigene Tabellen zurückgespielt", len(tables)))

	// The homeserver itself.
	if h.k8s == nil {
		job.say("skip", "ohne Cluster-Zugriff bleibt es bei MatrixCtrls eigenem Teil")
		return report, nil
	}
	admin, err := h.adminPG(ctx)
	if err != nil {
		return report, err
	}
	defer admin.Close(context.Background())

	cluster := restore.KubeCluster{K8s: h.k8s, Namespace: h.essNS}
	prog := restore.Progress(job.say)

	for _, p := range databaseParts(h.essRelease) {
		man, ok := parts[p.Prefix]
		if !ok {
			job.say("skip", fmt.Sprintf("%s ist nicht in diesem Archiv", p.Database))
			continue
		}
		// Keyed on the archive's format, not on a count of zero. A database can legitimately
		// have no sequences — the authentication service has none — and keying on the count
		// made this warn about a perfectly good format-2 archive (§4.107). The per-database
		// restore emits the authoritative message; this one only flags a genuinely old file.
		if full.FormatVersion < 2 {
			job.say("warn", fmt.Sprintf(
				"%s: dieses Archiv ist im alten Format und enthält die Zähler nicht — der Server "+
					"vergibt Stream-Nummern neu, die er schon vergeben hat", p.Database))
		}
		res, err := restore.Database(ctx, p, man, cluster, admin, stamp, prog,
			tableFeed(path, p.Prefix))
		if err != nil {
			return report, fmt.Errorf("%s: %w", p.Database, err)
		}
		report.Databases = append(report.Databases, res)
	}

	if opts.Media {
		n, err := h.restoreMedia(ctx, path, stamp, prog)
		if err != nil {
			// The databases are back and correct; the files are a separate part and
			// saying so is more useful than calling the whole thing a failure.
			job.say("media", "die Dateien konnten nicht zurückgespielt werden: "+err.Error())
		}
		report.MediaFiles = n
	}

	if opts.Keys {
		res, err := h.restoreKeys(ctx, path, opts.RecoveryKey, prog)
		if err != nil {
			job.say("keys", "die Schlüssel konnten nicht zurückgespielt werden: "+err.Error())
		} else if res != nil {
			report.Keys = res
		}
	}

	job.say("done", "fertig — die Dienste starten mit den zurückgespielten Daten")
	return report, nil
}

// databaseParts is which database lives under which prefix, and which workload has to
// be stopped for it.
//
// Synapse is a StatefulSet and the authentication service is a Deployment; handling only
// one kind is how a restore leaves the accounts untouched and reports success.
func databaseParts(release string) []restore.Part {
	return []restore.Part{
		{
			Prefix: "homeserver/", Database: "synapse", Owner: "synapse_user",
			Workload: restore.Workload{
				Kind: "statefulset", Name: release + "-synapse-main",
				Selector: "app.kubernetes.io/name=synapse-main",
			},
		},
		{
			Prefix: "mas/", Database: "matrixauthenticationservice", Owner: "matrixauthenticationservice_user",
			Workload: restore.Workload{
				Kind: "deployment", Name: release + "-matrix-authentication-service",
				Selector: "app.kubernetes.io/name=matrix-authentication-service",
			},
		},
	}
}

// tableFeed walks the spooled archive again and hands one part's CSVs over as streams.
//
// A fresh pass per part rather than one walk driving everything: the parts are restored
// one at a time, each with its own stop-swap-start around it, and a single walk would
// have to hold the tar open across all of that. Re-reading costs a decompression and
// buys a loop that can be read.
func tableFeed(path, prefix string) restore.Feed {
	return func(load func(string, io.Reader) error) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()

		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			table, ok := backup.TableFromArchivePath(h.Name, prefix)
			if !ok {
				continue
			}
			if err := load(table, tr); err != nil {
				return err
			}
		}
	}
}

// readFullManifest reads the archive's own account of itself, and each part's.
//
// One pass, reading only the manifests: everything else is skipped without being
// decompressed into memory, which is what makes this safe to do on an archive the size
// of a homeserver.
func readFullManifest(path string) (backup.FullManifest, map[string]backup.HomeserverManifest, error) {
	var full backup.FullManifest
	parts := map[string]backup.HomeserverManifest{}

	f, err := os.Open(path)
	if err != nil {
		return full, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return full, nil, fmt.Errorf("kein gültiges gzip-Archiv: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return full, nil, fmt.Errorf("beschädigtes Archiv: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, "manifest.json") {
			continue
		}
		if h.Name == "manifest.json" {
			if err := json.NewDecoder(tr).Decode(&full); err != nil {
				return full, nil, fmt.Errorf("Manifest unlesbar: %w", err)
			}
			continue
		}
		var man backup.HomeserverManifest
		if err := json.NewDecoder(tr).Decode(&man); err != nil {
			continue
		}
		parts[strings.TrimSuffix(h.Name, "manifest.json")] = man
	}
	if full.FormatVersion == 0 {
		return full, nil, fmt.Errorf("kein Manifest im Archiv — erwartet wird eine Datei aus " +
			"„Vollständiges Archiv\" auf dieser Seite")
	}
	if full.FormatVersion > backup.FormatVersion {
		return full, nil, fmt.Errorf("Archivformat %d, dieses MatrixCtrl versteht bis %d",
			full.FormatVersion, backup.FormatVersion)
	}
	return full, parts, nil
}

// memberReader positions a fresh read of the archive on one member and hands it over as
// a stream — for the media, which is a tar inside the tar and is never held anywhere.
func memberReader(path, name string) (io.ReadCloser, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			_ = f.Close()
			if err == io.EOF {
				return nil, 0, fmt.Errorf("%s ist nicht in diesem Archiv", name)
			}
			return nil, 0, err
		}
		if h.Name == name && h.Typeflag == tar.TypeReg {
			return readCloser{Reader: tr, closer: f}, h.Size, nil
		}
	}
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r readCloser) Close() error { return r.closer.Close() }

// adminPG opens the superuser connection a restore needs.
//
// Creating a database, renaming one and turning off constraint checks are all refused to
// synapse_user, which holds neither `createdb` nor `super`. The password is read from
// the cluster secret per restore and closed with it — the rule synapseDSN already
// follows, because a credential kept on a struct outlives the reason it was needed.
func (h *StatusHandler) adminPG(ctx context.Context) (*restore.AdminPG, error) {
	pw, err := h.k8s.SecretValue(ctx, h.essNS, "ess-generated", "POSTGRES_ADMIN_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("der Administrator-Zugang zur Datenbank ist nicht lesbar: %w", err)
	}
	dsn := fmt.Sprintf("postgres://postgres:%s@ess-postgres.%s.svc.cluster.local:5432/postgres?sslmode=disable",
		url.QueryEscape(pw), h.essNS)
	return restore.OpenAdmin(ctx, dsn)
}

// restoreMedia streams the media member of the archive back into the Synapse pod.
//
// After the databases, because it needs a running Synapse pod to exec into — and by then
// there is one. The stream never lands in this process: it goes from the spooled file
// through the tar reader into the pod's stdin.
func (h *StatusHandler) restoreMedia(ctx context.Context, path, stamp string, prog restore.Progress) (int64, error) {
	r, size, err := memberReader(path, "media/media.tar")
	if err != nil {
		prog("media", "keine Dateien in diesem Archiv")
		return 0, nil
	}
	defer r.Close()

	pod, err := h.synapsePod(ctx)
	if err != nil {
		return 0, err
	}
	return restore.Media(ctx, h.k8s, h.essNS, pod, "synapse", "/media", stamp, size, r, prog)
}

// restoreKeys puts the homeserver's identity back, if the operator supplied the recovery
// key that was shown once when the archive was made.
func (h *StatusHandler) restoreKeys(ctx context.Context, path, recoveryKey string, prog restore.Progress) (*restore.KeyResult, error) {
	r, _, err := memberReader(path, "secrets/sealed.bin")
	if err != nil {
		prog("keys", "keine Schlüssel in diesem Archiv")
		return nil, nil
	}
	defer r.Close()

	sealed, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(recoveryKey) == "" {
		return nil, fmt.Errorf("dieses Archiv enthält die Schlüssel des Homeservers, " +
			"und ohne den Wiederherstellungsschlüssel lassen sie sich nicht öffnen — " +
			"alles andere ist bereits zurückgespielt")
	}
	key, err := backup.ParseRecoveryKey(recoveryKey)
	if err != nil {
		return nil, err
	}
	res, err := restore.Keys(ctx, h.k8s, h.essNS, sealed, key, prog)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
