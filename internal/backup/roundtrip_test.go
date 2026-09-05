package backup

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/db"
)

// TestFullArchiveRoundTrip takes a real archive from a real database and restores it into
// a scratch one, then compares what arrived against what was written.
//
// The unit tests build archives by hand, which is exactly how etappe 72 shipped a restore
// that silently did nothing: the layout the *producer* wrote and the layout the *consumer*
// expected had drifted apart, and no hand-built fixture could notice because both sides of
// the fixture were written by the same assumption. This test never states the layout. It
// asks CreateFull for an archive and asks Read to understand it, so a producer change that
// the consumer has not followed fails here rather than during a restore.
//
// Skipped without a DSN so `go test ./...` stays hermetic:
//
//	MATRIXCTRL_BACKUP_TEST_DSN=postgres://…/matrixctrl go test ./internal/backup/ -run RoundTrip
//
// It creates and drops its own database and never writes to the one in the DSN.
func TestFullArchiveRoundTrip(t *testing.T) {
	dsn := os.Getenv("MATRIXCTRL_BACKUP_TEST_DSN")
	if dsn == "" {
		t.Skip("set MATRIXCTRL_BACKUP_TEST_DSN to run the round trip against a real database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	source, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to source: %v", err)
	}
	defer source.Close()

	// A config repo with a nested path and a dotfile: both are ways a tar walk can quietly
	// drop entries, and neither shows up in a flat fixture.
	repo := t.TempDir()
	files := map[string]string{
		"values.yaml":            "synapse:\n  replicas: 1\n",
		"slices/synapse.yaml":    "# a nested slice\nfoo: bar\n",
		".git/HEAD":              "ref: refs/heads/main\n",
		"slices/matrix-rtc.yaml": "rtc: {}\n",
	}
	for name, body := range files {
		full := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var archive bytes.Buffer
	ess := Release{Name: "ess", Namespace: "ess", Chart: "matrix-stack-25.7.1", Revision: 42}
	if err := CreateFull(ctx, source, nil, repo, "roundtrip", ess, &archive); err != nil {
		t.Fatalf("CreateFull: %v", err)
	}
	t.Logf("archive: %d bytes", archive.Len())

	got, err := Read(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatalf("Read rejected an archive this same package just wrote: %v", err)
	}
	if len(got.tables) == 0 {
		t.Fatal("no tables came back — a restore would report success and do nothing")
	}
	if len(got.config) != len(files) {
		t.Errorf("config files: wrote %d, read back %d", len(files), len(got.config))
	}
	if got.Manifest.ESS.Revision != ess.Revision || got.Manifest.ESS.Chart != ess.Chart {
		t.Errorf("the release identity did not survive: %+v", got.Manifest.ESS)
	}

	// Restore into a database of its own, created here and dropped at the end.
	scratch := fmt.Sprintf("matrixctrl_roundtrip_%d", os.Getpid())
	if _, err := source.Exec(ctx, `DROP DATABASE IF EXISTS `+scratch); err != nil {
		t.Fatalf("drop stale scratch database: %v", err)
	}
	if _, err := source.Exec(ctx, `CREATE DATABASE `+scratch); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		// Its own connection: the pool above is closed by a defer, and defers run before
		// cleanups, so reusing it leaves the scratch database behind on every run.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		drop, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Errorf("scratch database %s was left behind: %v", scratch, err)
			return
		}
		defer drop.Close()
		if _, err := drop.Exec(ctx, `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`); err != nil {
			t.Errorf("scratch database %s was left behind: %v", scratch, err)
		}
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + scratch
	// db.New runs the migrations, so the target has the schema an empty restore target has.
	target, err := db.New(ctx, u.String())
	if err != nil {
		t.Fatalf("prepare scratch database: %v", err)
	}
	defer target.Close()

	restored, err := got.RestoreDatabase(ctx, target)
	if err != nil {
		t.Fatalf("RestoreDatabase: %v", err)
	}
	if len(restored) == 0 {
		t.Fatal("restore touched no tables")
	}

	// The archive is the contract, not the source: comparing against the live database
	// would race with anything writing to it. Rows in the CSV must equal rows in the target.
	for name, csv := range got.tables {
		if neverRestored[name] {
			continue
		}
		want := strings.Count(strings.TrimRight(string(csv), "\n"), "\n") // lines minus header
		var have int
		if err := target.QueryRow(ctx, `SELECT count(*) FROM `+name).Scan(&have); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if want != have {
			t.Errorf("%s: archive holds %d rows, restore produced %d", name, want, have)
		}
	}

	// And the files, byte for byte.
	out := t.TempDir()
	n, err := got.RestoreConfigRepo(out)
	if err != nil {
		t.Fatalf("RestoreConfigRepo: %v", err)
	}
	if n != len(files) {
		t.Errorf("restored %d config files, wrote %d", n, len(files))
	}
	for name, body := range files {
		back, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Errorf("%s did not come back: %v", name, err)
			continue
		}
		if string(back) != body {
			t.Errorf("%s came back different", name)
		}
	}
	t.Logf("round trip: %d tables, %d config files", len(restored), n)
}
