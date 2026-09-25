package restore

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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// The whole swap, against a real Postgres.
//
// The unit tests pin the order of the steps; this one proves the steps do what their
// names say — that `ALTER DATABASE … RENAME` is accepted after the service is stopped,
// that a superuser can COPY into another role's tables, that the counters land, and that
// the old data is still there afterwards under its new name. None of that is decidable
// against a fake, and all of it is the difference between a restore and a lost server.
//
//	MATRIXCTRL_BACKUP_TEST_DSN=postgres://…/postgres go test ./internal/restore/ -run Live
//
// It creates and drops its own databases and never touches the one in the DSN.
func TestLiveDatabaseSwap(t *testing.T) {
	dsn := os.Getenv("MATRIXCTRL_BACKUP_TEST_DSN")
	if dsn == "" {
		t.Skip("set MATRIXCTRL_BACKUP_TEST_DSN to run against a real database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := OpenAdmin(ctx, dsn)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })

	const schema = `
		CREATE SEQUENCE events_stream_seq;
		CREATE TABLE events (stream_ordering bigint PRIMARY KEY, body text NOT NULL);`

	live := fmt.Sprintf("mxctrl_live_%d", os.Getpid())
	stamp := "testrun"
	dropLater(ctx, t, admin, live, live+"_vor_"+stamp)

	// The server as it is today: 300 events and a counter that has been used.
	if err := admin.Create(ctx, live, currentUser(ctx, t, admin)); err != nil {
		t.Fatal(err)
	}
	seed := connectTo(ctx, t, dsn, live)
	mustExec(ctx, t, seed, schema)
	for i := 0; i < 300; i++ {
		mustExec(ctx, t, seed, `INSERT INTO events (stream_ordering, body)
			VALUES (nextval('events_stream_seq'), 'before the restore')`)
	}

	// An archive of that state, written by the backup path, not by this test.
	var archive bytes.Buffer
	if err := backup.ExportHomeserver(ctx, seed, live, &archive); err != nil {
		t.Fatalf("export: %v", err)
	}
	seed.Close(ctx)

	// Then somebody deletes half of it — this is the disaster being recovered from.
	victim := connectTo(ctx, t, dsn, live)
	mustExec(ctx, t, victim, `DELETE FROM events WHERE stream_ordering > 150`)
	victim.Close(ctx)

	// The "service": stopping it does nothing, starting an empty database builds the
	// schema in it — which is exactly what Synapse does on first start, and the step the
	// archive deliberately does not carry (§4.66).
	svc := &schemaBuildingService{dsn: dsn, database: live, schema: schema, t: t}

	man, feed := partFeed(t, archive.Bytes(), "")
	var log []string
	res, err := Database(ctx, Part{
		Database: live, Owner: currentUser(ctx, t, admin),
		Workload: Workload{Kind: "statefulset", Name: "the-service", Selector: "app=x"},
	}, man, svc, admin, stamp, func(step, detail string) {
		log = append(log, step+": "+detail)
	}, feed)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, strings.Join(log, "\n"))
	}
	t.Logf("%s", strings.Join(log, "\n"))

	if res.TotalRows != 300 {
		t.Errorf("300 Zeilen erwartet, %d geladen", res.TotalRows)
	}
	if res.Sequences != 1 {
		t.Errorf("der Zähler muss gesetzt worden sein, %d gesetzt", res.Sequences)
	}

	// What the operator actually cares about: the deleted events are back, and the next
	// write continues the history instead of colliding with it.
	back := connectTo(ctx, t, dsn, live)
	defer back.Close(context.Background())

	var rows int64
	if err := back.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 300 {
		t.Errorf("nach der Wiederherstellung stehen %d Zeilen da, 300 erwartet", rows)
	}
	var next int64
	if err := back.QueryRow(ctx, `INSERT INTO events (stream_ordering, body)
		VALUES (nextval('events_stream_seq'), 'after') RETURNING stream_ordering`).Scan(&next); err != nil {
		t.Fatalf("der wiederhergestellte Server vergibt eine bereits benutzte Nummer: %v", err)
	}
	if next != 301 {
		t.Errorf("die nächste Nummer muss die Historie fortsetzen: %d", next)
	}

	// And the state that was replaced is still on disk, under the name the result names.
	if res.PreviousName == "" {
		t.Fatal("the result must name where the previous database went")
	}
	old := connectTo(ctx, t, dsn, res.PreviousName)
	defer old.Close(context.Background())
	var kept int64
	if err := old.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 150 {
		t.Errorf("die ersetzte Datenbank muss unverändert erhalten bleiben: %d Zeilen, 150 erwartet", kept)
	}
	t.Logf("die ersetzte Datenbank liegt unter %s und hält ihre %d Zeilen", res.PreviousName, kept)
}

// schemaBuildingService stands in for Synapse: it has no pods, and starting it against
// an empty database creates the schema there.
type schemaBuildingService struct {
	dsn, database, schema string
	t                     *testing.T
	running               bool
}

func (s *schemaBuildingService) Replicas(context.Context, string, string) (int32, error) {
	return 1, nil
}

func (s *schemaBuildingService) Scale(ctx context.Context, _, _ string, n int32) error {
	if n == 0 {
		s.running = false
		return nil
	}
	s.running = true
	conn, err := pgx.Connect(ctx, withDatabase(s.t, s.dsn, s.database))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	// Only if it is empty — a real service migrates rather than recreating, and a test
	// double that wipes the database it is handed would hide a restore that never loaded.
	var tables int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		_, err = conn.Exec(ctx, s.schema)
	}
	return err
}

func (s *schemaBuildingService) WaitGone(context.Context, string) error          { return nil }
func (s *schemaBuildingService) WaitReady(context.Context, string, string) error { return nil }

// partFeed walks an archive the way the handler will: once, in order, streaming.
func partFeed(t *testing.T, archive []byte, prefix string) (backup.HomeserverManifest, Feed) {
	t.Helper()
	var man backup.HomeserverManifest

	// The manifest is read first so the caller knows the counters before the load.
	walk(t, archive, func(name string, r io.Reader) error {
		if name == prefix+"manifest.json" {
			return json.NewDecoder(r).Decode(&man)
		}
		return nil
	})

	return man, func(load func(string, io.Reader) error) error {
		return walk(t, archive, func(name string, r io.Reader) error {
			table, ok := backup.TableFromArchivePath(name, prefix)
			if !ok {
				return nil
			}
			return load(table, r)
		})
	}
}

func walk(t *testing.T, archive []byte, each func(name string, r io.Reader) error) error {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
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
		if err := each(h.Name, tr); err != nil {
			return err
		}
	}
}

func dropLater(ctx context.Context, t *testing.T, admin *AdminPG, names ...string) {
	t.Helper()
	for _, n := range names {
		_ = admin.Drop(ctx, n)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, n := range names {
			_ = admin.Terminate(ctx, n)
			if err := admin.Drop(ctx, n); err != nil {
				t.Errorf("scratch database %s was left behind: %v", n, err)
			}
		}
	})
}

func connectTo(ctx context.Context, t *testing.T, dsn, database string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, withDatabase(t, dsn, database))
	if err != nil {
		t.Fatalf("connect to %s: %v", database, err)
	}
	return conn
}

func withDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + database
	return u.String()
}

func mustExec(ctx context.Context, t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0], err)
	}
}

func currentUser(ctx context.Context, t *testing.T, admin *AdminPG) string {
	t.Helper()
	conn := connectTo(ctx, t, admin.DSN, "postgres")
	defer conn.Close(context.Background())
	var who string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&who); err != nil {
		t.Fatal(err)
	}
	return who
}
