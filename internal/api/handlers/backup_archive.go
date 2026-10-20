package handlers

import (
	"context"
	"io"
	"log"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// One place that knows what a full archive is made of (etappe 120).
//
// The browser download and the unattended backup to S3 must produce the same archive; two
// copies of "connect to Synapse, connect to MAS, read the keys, tar the media" would drift
// the first time a part is added to one of them. Only two things differ, and they are
// parameters: whether the media come along, and how the keys are protected.

type archiveSecrets int

const (
	// secretsNone leaves the homeserver's keys out.
	secretsNone archiveSecrets = iota
	// secretsSealed seals them with a one-off recovery key shown to the operator (the
	// download, where somebody is watching).
	secretsSealed
	// secretsPlain stores them as they are, for an archive that is encrypted as a whole.
	secretsPlain
)

// assembleArchive opens the connections and parts and returns the options for
// backup.CreateFull. cleanup must be called when the archive has been written (or the
// attempt abandoned): it closes the database connections and the media pipe.
//
// Parts that cannot be reached are left out and logged, as the download always did: an
// archive missing Synapse's database with a manifest saying so beats no archive. The
// manifest records it either way.
func (h *StatusHandler) assembleArchive(ctx context.Context, media bool, secrets archiveSecrets) (opts backup.FullOptions, shownKey string, cleanup func()) {
	var closers []func()
	cleanup = func() {
		for _, c := range closers {
			c()
		}
	}

	var hs *pgx.Conn
	if h.k8s != nil {
		if dsn, err := h.synapseDSN(ctx); err == nil {
			if conn, cerr := pgx.Connect(ctx, dsn); cerr == nil {
				hs = conn
				closers = append(closers, func() { conn.Close(context.Background()) })
			} else {
				// Logged, never echoed: the message carries the DSN and the DSN carries
				// the password.
				log.Printf("full backup: synapse unreachable, continuing without it: %v", cerr)
			}
		}
	}

	// The accounts. Under MSC3861 they live in the authentication service's database,
	// not Synapse's — which is why a "full" archive used to bring the rooms back and
	// nobody who could log in to them (etappe 102).
	var mas *pgx.Conn
	if h.k8s != nil {
		if dsn, err := h.masDSN(ctx); err == nil {
			if conn, cerr := pgx.Connect(ctx, dsn); cerr == nil {
				mas = conn
				closers = append(closers, func() { conn.Close(context.Background()) })
			} else {
				log.Printf("full backup: MAS database unreachable, continuing without the accounts: %v", cerr)
			}
		}
	}

	opts = backup.FullOptions{
		DB:         h.backupDB,
		Homeserver: hs,
		MAS:        mas,
		ConfigRepo: h.backupRepo,
		AppVersion: h.appVersion,
		ESS:        h.essRelease_(),
	}

	// The keys. Sealed: the key is generated per archive, returned in a header and stored
	// nowhere — see internal/backup/seal.go for why that is the only placement that works
	// for a download. Plain: the archive around them is encrypted already.
	if secrets != secretsNone && h.k8s != nil {
		blob, err := h.k8s.SecretYAML(ctx, h.essNS, "ess-generated")
		switch {
		case err != nil:
			log.Printf("full backup: could not read ess-generated, continuing without the keys: %v", err)
		case secrets == secretsPlain:
			opts.PlainSecrets = blob
		default:
			key, shown, kerr := backup.NewRecoveryKey()
			if kerr == nil {
				if sealed, serr := backup.Seal(key, blob); serr == nil {
					opts.Sealed = sealed
					shownKey = shown
				} else {
					log.Printf("full backup: sealing the keys failed, continuing without them: %v", serr)
				}
			}
		}
	}

	// The uploaded files, off the volume only Synapse mounts. Opt-in, because they can
	// be hundreds of gigabytes.
	if media && h.k8s != nil {
		if pod, perr := h.synapsePod(ctx); perr == nil {
			pr, pw := io.Pipe()
			release := make(chan struct{})
			m := &mediaSource{r: pr, release: release}
			closers = append(closers, func() { m.EndOfArchive(); _ = pr.Close() })
			// Started on the first read, not here: the media are the last large part,
			// and there is no reason to hold an exec open behind the database dumps.
			// Held open after tar until the end marker is read — see TarFromPod for the
			// stream that otherwise drops its tail and reports success.
			m.start = func() {
				go func() {
					err := h.k8s.TarFromPod(context.Background(), h.essNS, pod, "synapse", "/media", pw, release)
					_ = pw.CloseWithError(err)
				}()
			}
			opts.Media = m
		} else {
			log.Printf("full backup: no synapse pod for the media, continuing without them: %v", perr)
		}
	}
	return opts, shownKey, cleanup
}

// WriteOffsiteArchive writes the archive for an unattended backup: the keys unsealed (the
// whole stream is encrypted by the caller), media as asked.
func (h *StatusHandler) WriteOffsiteArchive(ctx context.Context, media bool, w io.Writer) error {
	if h.backupDB == nil {
		return errBackupUnavailable
	}
	opts, _, cleanup := h.assembleArchive(ctx, media, secretsPlain)
	defer cleanup()
	return backup.CreateFull(ctx, opts, w)
}

// mediaSource starts the media exec on the first read and releases it once the archive's
// end marker has been read (backup.EndOfArchiveNotifier).
type mediaSource struct {
	r        io.Reader
	start    func()
	started  sync.Once
	release  chan struct{}
	released sync.Once
}

func (m *mediaSource) Read(p []byte) (int, error) {
	m.started.Do(m.start)
	return m.r.Read(p)
}

func (m *mediaSource) EndOfArchive() { m.released.Do(func() { close(m.release) }) }
