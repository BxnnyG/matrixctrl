package offsite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// RunOptions is one backup run.
type RunOptions struct {
	Store     Store
	Prefix    string
	PublicKey string
	Occasion  string
	// Now names the object; injectable so tests do not depend on the minute they run in.
	Now time.Time
	// Build writes the plain tar.gz archive. It is the same function the browser
	// download uses (etappe 120: one place that knows what a backup is made of).
	Build func(ctx context.Context, w io.Writer) error
	// Progress, if set, is told how many encrypted bytes have gone out, at most every few
	// seconds and once at the end.
	Progress func(bytes int64)
}

// Result is what a successful run left behind.
type Result struct {
	Key      string        `json:"key"`
	Size     int64         `json:"size"`
	Duration time.Duration `json:"-"`
}

// Run builds the archive, encrypts it and uploads it, as one stream.
//
// The failure case is the design. A backup that dies halfway must not leave an object
// that looks like a backup: in a bucket, a truncated file with a valid name is worse than
// no file, because the next restore finds it and trusts it. So the error of the build
// travels through the pipe as the read error of the upload, which makes S3 abandon the
// multipart upload — and if a store ever completes an upload anyway, the object is
// removed again here rather than reported as a success.
func Run(ctx context.Context, o RunOptions) (Result, error) {
	start := time.Now()
	prefix, err := NormalizePrefix(o.Prefix)
	if err != nil {
		return Result{}, err
	}
	// Checked before anything is built: a bad key must cost nothing.
	if _, err := ParsePublicKey(o.PublicKey); err != nil {
		return Result{}, err
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	name, err := ObjectName(now, o.Occasion)
	if err != nil {
		return Result{}, err
	}
	key := joinKey(prefix, name)

	bctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	built := make(chan error, 1)
	go func() {
		err := func() error {
			enc, err := Encrypt(pw, o.PublicKey)
			if err != nil {
				return err
			}
			if err := o.Build(bctx, enc); err != nil {
				// Not closing enc is intended: the final chunk is what marks an age
				// stream complete, and this one is not.
				return err
			}
			return enc.Close()
		}()
		// nil → the reader sees EOF; otherwise it sees this error and the upload fails.
		_ = pw.CloseWithError(err)
		built <- err
	}()

	var sent atomic.Int64
	var lastReport time.Time
	body := readerFunc(func(p []byte) (int, error) {
		n, err := pr.Read(p)
		total := sent.Add(int64(n))
		if o.Progress != nil && time.Since(lastReport) > 5*time.Second {
			lastReport = time.Now()
			o.Progress(total)
		}
		return n, err
	})

	size, putErr := o.Store.Put(ctx, key, body)
	if putErr != nil {
		// Unblocks the build if it is still writing, and stops its work.
		_ = pr.CloseWithError(putErr)
		cancel()
		berr := <-built
		// A build that fails first reaches the store as a read error, so the upload's
		// error is its echo; the build's own error is the cause worth reporting. One that
		// only failed because the pipe was closed on it is the echo the other way round.
		if berr != nil && !errors.Is(berr, putErr) && !errors.Is(berr, io.ErrClosedPipe) && !errors.Is(berr, context.Canceled) {
			return Result{}, fmt.Errorf("Archiv: %w", berr)
		}
		return Result{}, fmt.Errorf("Upload: %w", putErr)
	}
	if berr := <-built; berr != nil {
		// The store accepted a stream that the build had already declared broken.
		rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer rcancel()
		if rerr := o.Store.Remove(rctx, key); rerr != nil {
			return Result{}, fmt.Errorf("Archiv: %v; das unvollständige Objekt %q konnte nicht entfernt werden: %v", berr, key, rerr)
		}
		return Result{}, fmt.Errorf("Archiv: %w", berr)
	}
	if o.Progress != nil {
		o.Progress(sent.Load())
	}
	return Result{Key: key, Size: size, Duration: time.Since(start)}, nil
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// ListEntries returns our backups under prefix, newest first. Everything that is not
// ours is left out here, which is what keeps every caller from touching it.
func ListEntries(ctx context.Context, st Store, prefix string) ([]Entry, error) {
	prefix, err := NormalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	listPrefix := prefix
	if prefix != "" {
		listPrefix += "/"
	}
	objs, err := st.List(ctx, listPrefix)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, o := range objs {
		if e, ok := entryFor(prefix, o); ok {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// Open streams one of our backups, still encrypted. The name must be one of ours: this is
// reachable from a request, and it must not become a way to read other things in the
// bucket.
func Open(ctx context.Context, st Store, prefix, name string) (io.ReadCloser, int64, error) {
	prefix, err := NormalizePrefix(prefix)
	if err != nil {
		return nil, 0, err
	}
	if _, _, ok := ParseName(name); !ok {
		return nil, 0, errors.New("Das ist kein Backup dieser Installation.")
	}
	return st.Get(ctx, joinKey(prefix, name))
}

// Prune deletes the backups the policy no longer keeps and returns their keys. It only
// ever considers entries ListEntries recognises, so a foreign object is never a
// candidate. It carries on past a failed delete and reports the first failure.
func Prune(ctx context.Context, st Store, prefix string, now time.Time, p Policy) ([]string, error) {
	entries, err := ListEntries(ctx, st, prefix)
	if err != nil {
		return nil, err
	}
	_, drop := Retain(entries, now, p)
	var deleted []string
	var first error
	for _, e := range drop {
		if err := st.Remove(ctx, e.Key); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", e.Name, err)
			}
			continue
		}
		deleted = append(deleted, e.Key)
	}
	// Said in the log: a backup that disappears without a line anywhere is how an
	// operator finds out during a restore.
	if len(deleted) > 0 {
		log.Printf("offsite: retention removed %d backup(s): %s", len(deleted), strings.Join(deleted, ", "))
	}
	return deleted, first
}

// ProbeStep is one stage of the connection test.
type ProbeStep struct {
	Step  string `json:"step"` // schreiben | lesen | löschen
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ProbeResult is the answer to "does this target work".
type ProbeResult struct {
	OK    bool        `json:"ok"`
	Steps []ProbeStep `json:"steps"`
}

// Probe writes a small object, reads it back and deletes it. Listing alone would say
// nothing about whether a night's backup can be stored, and a store that allows writing
// but not deleting would pass an upload test and then grow without bound because Prune
// can never remove anything — so deleting is part of the test. The object's name does not
// match the backup pattern, so no listing shows it as a backup.
func Probe(ctx context.Context, st Store, prefix string, now time.Time) ProbeResult {
	prefix, err := NormalizePrefix(prefix)
	res := ProbeResult{}
	if err != nil {
		res.Steps = []ProbeStep{{Step: "schreiben", Error: err.Error()}}
		return res
	}
	key := joinKey(prefix, fmt.Sprintf(".matrixctrl-verbindungstest-%d", now.UnixNano()))
	payload := []byte("matrixctrl verbindungstest " + now.UTC().Format(time.RFC3339Nano))

	step := func(name string, fn func() error) bool {
		err := fn()
		s := ProbeStep{Step: name, OK: err == nil}
		if err != nil {
			s.Error = err.Error()
		}
		res.Steps = append(res.Steps, s)
		return err == nil
	}

	wrote := step("schreiben", func() error {
		_, err := st.Put(ctx, key, bytes.NewReader(payload))
		return err
	})
	if !wrote {
		return res
	}
	// Whatever happens next, try to remove what was written.
	readOK := step("lesen", func() error {
		rc, _, err := st.Get(ctx, key)
		if err != nil {
			return err
		}
		defer rc.Close()
		got, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		if err != nil {
			return err
		}
		if string(got) != string(payload) {
			return errors.New("Der Anbieter hat etwas anderes zurückgegeben, als geschrieben wurde.")
		}
		return nil
	})
	removed := step("löschen", func() error { return st.Remove(ctx, key) })
	res.OK = wrote && readOK && removed
	return res
}
