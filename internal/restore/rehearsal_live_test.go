package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bxnnyg/matrixctrl/internal/backup"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// The whole run, against the real ESS: stop, swap, load, start, count.
//
// Everything else in this package is proven in pieces — the order against fakes, the
// swap against a real Postgres, stopping and waiting against a real cluster. A
// homeserver restore is the one operation where the pieces working says little about
// the whole: what breaks is the interaction, and it breaks at the step nobody rehearsed.
//
// So it is rehearsed, and the rehearsal is opt-in with a variable of its own. RUN_LIVE=1
// must never reach this: a test that stops somebody's Synapse has no business running
// because another test wanted to read a pod log.
//
//	MATRIXCTRL_REHEARSE_RESTORE=1 go test ./internal/restore/ -run Rehearsal -v -timeout 30m
//
// It exports the current state first and restores *that*, so a successful run changes
// nothing and a failed one is recoverable from the files it just wrote. The operator
// agreed to it on 2026-09-25, on the server whose DNS has already moved away.
func TestRehearsalAgainstLiveESS(t *testing.T) {
	if os.Getenv("MATRIXCTRL_REHEARSE_RESTORE") == "" {
		t.Skip("set MATRIXCTRL_REHEARSE_RESTORE=1 — this stops Synapse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	c, err := k8s.New()
	if err != nil {
		t.Fatal(err)
	}
	ns := "ess"
	release := "ess"

	pw, err := c.SecretValue(ctx, ns, "ess-generated", "POSTGRES_ADMIN_PASSWORD")
	if err != nil {
		t.Fatalf("admin password: %v", err)
	}
	host := os.Getenv("MATRIXCTRL_PG_HOST")
	if host == "" {
		host = "127.0.0.1:5432" // via a port-forward to ess-postgres
	}
	dsn := fmt.Sprintf("postgres://postgres:%s@%s/postgres?sslmode=disable", pw, host)

	admin, err := OpenAdmin(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connection: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })

	outDir := os.Getenv("MATRIXCTRL_REHEARSAL_DIR")
	if outDir == "" {
		outDir = "/root/rehearsal-" + time.Now().UTC().Format("20060102-1504")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Logf("die Sicherungen dieses Laufs liegen in %s", outDir)

	// The smaller database first. If the accounts do not survive, Synapse has not been
	// touched yet — a canary is only a canary if it goes in front.
	for _, p := range []Part{
		{
			Prefix: "", Database: "matrixauthenticationservice", Owner: "matrixauthenticationservice_user",
			Workload: Workload{
				Kind: "deployment", Name: release + "-matrix-authentication-service",
				Selector: "app.kubernetes.io/name=matrix-authentication-service",
			},
		},
		{
			Prefix: "", Database: "synapse", Owner: "synapse_user",
			Workload: Workload{
				Kind: "statefulset", Name: release + "-synapse-main",
				Selector: "app.kubernetes.io/name=synapse-main",
			},
		},
	} {
		t.Run(p.Database, func(t *testing.T) {
			before := tableCounts(ctx, t, dsn, p.Database)

			file := filepath.Join(outDir, p.Database+".tar.gz")
			f, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := pgx.Connect(ctx, withDatabase(t, dsn, p.Database))
			if err != nil {
				t.Fatalf("connect for the export: %v", err)
			}
			if err := backup.ExportHomeserver(ctx, conn, p.Database, f); err != nil {
				t.Fatalf("export: %v", err)
			}
			conn.Close(ctx)
			_ = f.Close()
			st, _ := os.Stat(file)
			t.Logf("gesichert: %s (%.1f MB)", file, float64(st.Size())/1024/1024)

			blob, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			man, feed := partFeed(t, blob, "")
			if len(man.Sequences) == 0 && p.Database == "synapse" {
				t.Fatal("der Export enthält keine Zähler — genau das repariert diese Etappe")
			}
			t.Logf("%d Tabellen, %d Zähler im Archiv", len(man.Tables), len(man.Sequences))

			start := time.Now()
			res, err := Database(ctx, p, man, KubeCluster{K8s: c, Namespace: ns}, admin,
				"probe"+time.Now().UTC().Format("0102_1504"),
				func(step, detail string) { t.Logf("  %s: %s", step, detail) }, feed)
			if err != nil {
				t.Fatalf("Wiederherstellung: %v", err)
			}
			t.Logf("%s in %s: %d Tabellen, %d Zeilen, %d Zähler — vorher liegt unter %s",
				p.Database, time.Since(start).Round(time.Second),
				res.Tables, res.TotalRows, res.Sequences, res.PreviousName)

			// What the operator cares about, asked of the database rather than of the
			// restore: is every table back at the count it had?
			// Losses are the failure; growth is not. The service is running again by
			// the time this counts, and a homeserver writes to its own stream tables the
			// moment it starts — a check that calls that a defect is a check that gets
			// switched off, and takes the real finding with it (§4.96). The first
			// rehearsal reported five "mismatches": three were a genuine defect (the
			// user directory being rebuilt from scratch, §4.106) and two were Synapse
			// writing two cache invalidations on startup.
			after := tableCounts(ctx, t, dsn, p.Database)
			var lost, grew []string
			for table, n := range before {
				if backup.BelongsToTheTarget[table] {
					continue
				}
				switch {
				case after[table] < n:
					lost = append(lost, fmt.Sprintf("%s: vorher %d, jetzt %d", table, n, after[table]))
				case after[table] > n:
					grew = append(grew, fmt.Sprintf("%s +%d", table, after[table]-n))
				}
			}
			if len(grew) > 0 {
				t.Logf("seit dem Start dazugeschrieben: %v", grew)
			}
			if len(lost) > 0 {
				t.Errorf("%d Tabellen haben Zeilen verloren:\n  %v", len(lost), lost)
			} else {
				t.Logf("keine Tabelle hat Zeilen verloren (%d geprüft)", len(before))
			}

			// The finding that made this rehearsal worth running: a database built from
			// scratch queues every initial background job, and keeping that list would
			// set the restored server to redoing work its data has long had done.
			var pending int64
			check := connectTo(ctx, t, dsn, p.Database)
			if err := check.QueryRow(ctx, `SELECT count(*) FROM background_updates`).Scan(&pending); err == nil {
				t.Logf("offene Hintergrundaufgaben nach der Wiederherstellung: %d", pending)
				if pending > 0 {
					t.Errorf("%d Hintergrundaufgaben sind offen — das Archiv kam von einem Server, "+
						"der sie erledigt hatte", pending)
				}
			}
			check.Close(context.Background())
		})
	}
}

// tableCounts counts every row in every table, exactly rather than from the statistics.
//
// pg_stat_user_tables is an estimate, and an estimate that happens to match is not
// evidence. This is the slow way and the only one that answers the question.
func tableCounts(ctx context.Context, t *testing.T, dsn, database string) map[string]int64 {
	t.Helper()
	conn := connectTo(ctx, t, dsn, database)
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()

	out := make(map[string]int64, len(names))
	for _, name := range names {
		var n int64
		if err := conn.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s`, pgx.Identifier{name}.Sanitize())).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		out[name] = n
	}
	return out
}
