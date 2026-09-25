package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The counters, end to end, against a real Postgres.
//
// Etappe 106 exists because the archive carried 173 tables and none of the 25 sequences
// Synapse draws its stream orderings and state-group ids from. A unit test cannot show
// what that costs: the rows all arrive, every statement succeeds, and the damage only
// appears the next time the server asks for a number. So this test asks for a number.
//
// It builds a source database with a counter that has been used, exports it the way a
// backup does, restores it the way a restore does, and then **inserts a row** into the
// restored database. Without the sequences that insert collides with a row that is
// already there. With them it does not. That is the whole etappe in one assertion.
//
//	MATRIXCTRL_BACKUP_TEST_DSN=postgres://…/postgres go test ./internal/backup/ -run Sequences
//
// It creates and drops its own databases and never writes to the one in the DSN.
func TestSequencesSurviveARestore(t *testing.T) {
	dsn := os.Getenv("MATRIXCTRL_BACKUP_TEST_DSN")
	if dsn == "" {
		t.Skip("set MATRIXCTRL_BACKUP_TEST_DSN to run against a real database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Registered as a cleanup rather than deferred: t.Cleanup runs in reverse order of
	// registration, and a deferred close would run *before* the scratch databases are
	// dropped, leaving them behind on every run with "conn closed" as the reason.
	t.Cleanup(func() { admin.Close(context.Background()) })

	source := scratchDB(ctx, t, admin, dsn, "mxctrl_seq_src")
	target := scratchDB(ctx, t, admin, dsn, "mxctrl_seq_dst")

	// A table whose primary key comes out of a sequence, in both databases — the shape
	// Synapse's stream tables have, and the shape that makes a missing counter fatal.
	const schema = `
		CREATE SEQUENCE events_stream_seq;
		CREATE SEQUENCE never_used_seq;
		CREATE TABLE events (stream_ordering bigint PRIMARY KEY, body text NOT NULL);`
	for _, c := range []*pgx.Conn{source, target} {
		if _, err := c.Exec(ctx, schema); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	// 500 events, each one taking its id from the counter, so the counter is at 500 and
	// the rows occupy 1..500.
	for i := 0; i < 500; i++ {
		if _, err := source.Exec(ctx,
			`INSERT INTO events (stream_ordering, body) VALUES (nextval('events_stream_seq'), $1)`,
			fmt.Sprintf("event %d", i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var archive bytes.Buffer
	if err := ExportHomeserver(ctx, source, "source", &archive); err != nil {
		t.Fatalf("export: %v", err)
	}
	t.Logf("archive: %d bytes", archive.Len())

	man, rows := loadArchiveInto(ctx, t, target, archive.Bytes(), "")

	if len(man.Sequences) == 0 {
		t.Fatal("the archive carries no sequences — this is the defect etappe 106 is about")
	}
	t.Logf("%d Tabellen, %d Sequenzen im Archiv", len(man.Tables), len(man.Sequences))
	if rows["events"] != 500 {
		t.Fatalf("events: 500 erwartet, %d geladen", rows["events"])
	}

	loader := NewLoader(target)
	set, skipped, err := loader.SetSequences(ctx, man.Sequences)
	if err != nil {
		t.Fatalf("setval: %v", err)
	}
	t.Logf("%d Sequenzen gesetzt, %d übersprungen %v", set, len(skipped), skipped)
	if set != 1 {
		t.Errorf("exactly one counter had been used and must be set, got %d", set)
	}

	// The assertion the whole etappe is for: the restored server hands out a number
	// nobody has used. Without SetSequences this insert fails on the primary key — and
	// in Synapse it would not fail at all, it would sort the new event before the entire
	// restored history.
	var next int64
	if err := target.QueryRow(ctx,
		`INSERT INTO events (stream_ordering, body) VALUES (nextval('events_stream_seq'), 'after the restore')
		 RETURNING stream_ordering`).Scan(&next); err != nil {
		t.Fatalf("the restored database handed out an id that was already taken: %v", err)
	}
	if next != 501 {
		t.Errorf("the next id must continue the history: got %d, want 501", next)
	}

	// And the counterpart, so the assertion above is known to be able to fail: the same
	// restore without the counters collides on the first insert.
	other := scratchDB(ctx, t, admin, dsn, "mxctrl_seq_nocounter")
	if _, err := other.Exec(ctx, schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	loadArchiveInto(ctx, t, other, archive.Bytes(), "")
	var ignored int64
	err = other.QueryRow(ctx,
		`INSERT INTO events (stream_ordering, body) VALUES (nextval('events_stream_seq'), 'no counters')
		 RETURNING stream_ordering`).Scan(&ignored)
	if err == nil {
		t.Fatal("without the sequences this insert must collide — if it does not, " +
			"the test above proves nothing about what the sequences are for")
	}
	t.Logf("counter-check: without the counters the first write fails, as it must: %v", err)
}

// loadArchiveInto walks an archive the way a restore does and fills a database from it.
func loadArchiveInto(ctx context.Context, t *testing.T, conn *pgx.Conn, archive []byte, prefix string) (HomeserverManifest, map[string]int64) {
	t.Helper()

	loader := NewLoader(conn)
	if err := loader.DeferConstraints(ctx); err != nil {
		t.Fatalf("defer constraints: %v", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()

	var man HomeserverManifest
	rows := map[string]int64{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("archive: %v", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Name == prefix+"manifest.json" {
			if err := json.NewDecoder(tr).Decode(&man); err != nil {
				t.Fatalf("manifest: %v", err)
			}
			continue
		}
		table, ok := TableFromArchivePath(h.Name, prefix)
		if !ok {
			continue
		}
		// Streamed straight from the tar into COPY, which is what the restore does and
		// the reason nothing here is held in memory.
		n, err := loader.LoadCSV(ctx, table, tr)
		if err != nil {
			t.Fatalf("load %s: %v", table, err)
		}
		rows[table] = n
	}

	bad, err := loader.Verify(ctx, rows)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(bad) > 0 {
		t.Fatalf("the database does not hold what was written: %v", bad)
	}
	return man, rows
}

// scratchDB creates a database of its own and drops it afterwards.
func scratchDB(ctx context.Context, t *testing.T, admin *pgx.Conn, dsn, name string) *pgx.Conn {
	t.Helper()
	name = fmt.Sprintf("%s_%d", name, os.Getpid())

	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoted(name)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop stale %s: %v", name, err)
	}
	if err := CreateDatabase(ctx, admin, name, currentUser(ctx, t, admin)); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoted(name)+` WITH (FORCE)`); err != nil {
			t.Errorf("scratch database %s was left behind: %v", name, err)
		}
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func currentUser(ctx context.Context, t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var who string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&who); err != nil {
		t.Fatalf("current_user: %v", err)
	}
	return who
}
