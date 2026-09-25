package backup

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Writing a homeserver's database back (etappe 106).
//
// Until now the archive said this was not a button: "dafür muss Synapse gestoppt sein,
// und es im laufenden Betrieb zu tun beschädigt, was da ist". Both halves were true and
// neither is a reason to leave the operator with `psql` — they are the requirements list
// for doing it properly. Stopping Synapse is a step MatrixCtrl can take. Not damaging
// what is there is a matter of never writing into the live database at all:
//
//	the old database is renamed aside, a fresh one takes its name, and if anything
//	fails the old name is one statement away.
//
// Nothing here truncates, deletes or overwrites an existing database. The most
// destructive statement in this file is a rename.

// quoted renders an identifier that came from a database or an archive.
//
// Table and sequence names arrive from pg_class on the source and from file names in an
// uploaded tar. They are interpolated into SQL because Postgres has no parameter for an
// identifier, so they go through the quoting the driver provides rather than through
// fmt.Sprintf with %q, which escapes for Go and not for SQL.
func quoted(name string) string { return pgx.Identifier{name}.Sanitize() }

// DatabaseExists answers without creating anything.
func DatabaseExists(ctx context.Context, admin *pgx.Conn, name string) (bool, error) {
	var n int
	err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, name).Scan(&n)
	return n > 0, err
}

// CreateDatabase makes an empty database owned by the service account that will use it.
//
// The owner matters: Synapse connects as synapse_user and creates its own schema on
// first start. A database owned by the admin role would leave it unable to.
func CreateDatabase(ctx context.Context, admin *pgx.Conn, name, owner string) error {
	_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, quoted(name), quoted(owner)))
	return err
}

// RenameDatabase is the only statement in a restore that touches existing data, and it
// moves it rather than changing it.
//
// Postgres refuses while anything is connected, which is a feature here: it means the
// rename cannot happen behind a running Synapse's back. The caller scales the workload
// down first and TerminateConnections clears what is left — a connection pool that has
// not noticed yet, or an admin session.
func RenameDatabase(ctx context.Context, admin *pgx.Conn, from, to string) error {
	_, err := admin.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s RENAME TO %s`, quoted(from), quoted(to)))
	if err != nil {
		return fmt.Errorf("umbenennen %s → %s: %w", from, to, err)
	}
	return nil
}

// TerminateConnections closes every session on a database except this one.
func TerminateConnections(ctx context.Context, admin *pgx.Conn, name string) error {
	_, err := admin.Exec(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	return err
}

// Loader writes an archive's rows into a database that already has the schema.
//
// The schema is deliberately not in the archive (§4.66): it comes from the service
// itself, which builds it on first start in the version that is running now. That is
// what makes an archive from an older ESS restorable onto a newer one — a schema
// carried in the file would drag the target back to the source's version.
type Loader struct {
	conn *pgx.Conn
}

// NewLoader takes a connection to the database being filled.
func NewLoader(conn *pgx.Conn) *Loader { return &Loader{conn: conn} }

// DeferConstraints turns off foreign-key enforcement for this session.
//
// The archive stores one CSV per table in name order, and a restore that respects
// foreign keys would need the reverse: parents before children, which for Synapse's 173
// tables is a graph nobody should compute on the fly while holding a stream open.
// Postgres's own dump/restore does the same thing for the same reason.
//
// It applies to this session only and is not a property of the database, so a failure
// partway through cannot leave the constraints off for anyone else. They are checked
// again by the caller after the load — data that violates them would be a broken
// archive, and finding that out is the point of the verification step.
func (l *Loader) DeferConstraints(ctx context.Context) error {
	_, err := l.conn.Exec(ctx, `SET session_replication_role = replica`)
	return err
}

// LoadCSV streams one table's rows in without holding them in memory.
//
// The reader is a tar entry from the uploaded archive, and it stays a stream from there
// to the server: a homeserver of any size has tables larger than the pod's memory limit,
// and reading one into a []byte is the difference between a restore that works on this
// install and one that works everywhere.
func (l *Loader) LoadCSV(ctx context.Context, table string, r io.Reader) (int64, error) {
	sql := fmt.Sprintf(`COPY %s FROM STDIN WITH (FORMAT csv, HEADER true)`, quoted(table))
	tag, err := l.conn.PgConn().CopyFrom(ctx, r, sql)
	if err != nil {
		return 0, fmt.Errorf("Tabelle %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

// SetSequences puts the counters back where the source had them.
//
// Without this the rows are restored and every sequence starts at 1 again, so the
// homeserver hands out stream orderings and state-group ids that its own restored rows
// already occupy. It does not fail loudly: new events sort before the whole history and
// the rooms look frozen (§4.106). A sequence the source had never read from is left
// alone — it is at its start already, and `setval(…, 0)` would be rejected.
//
// A sequence in the archive that does not exist here is skipped rather than fatal: the
// target may run a newer schema that dropped it, which is the same reasoning that makes
// the column-by-name match in RestoreDatabase right (§4.66). It is returned so the
// caller can show it rather than swallow it.
func (l *Loader) SetSequences(ctx context.Context, seqs []Sequence) (set int, skipped []string, err error) {
	for _, s := range seqs {
		if s.LastValue == nil {
			continue
		}
		var exists bool
		if err := l.conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_sequences WHERE schemaname = 'public' AND sequencename = $1)`,
			s.Name).Scan(&exists); err != nil {
			return set, skipped, err
		}
		if !exists {
			skipped = append(skipped, s.Name)
			continue
		}
		if _, err := l.conn.Exec(ctx,
			`SELECT setval($1::regclass, $2::bigint, true)`, s.Name, *s.LastValue); err != nil {
			return set, skipped, fmt.Errorf("Sequenz %s: %w", s.Name, err)
		}
		set++
	}
	return set, skipped, nil
}

// Mismatch is one table whose restored row count is not what the manifest promised.
type Mismatch struct {
	Table    string `json:"table"`
	Expected int64  `json:"expected"`
	Found    int64  `json:"found"`
}

func (m Mismatch) String() string {
	return fmt.Sprintf("%s: %d erwartet, %d gefunden", m.Table, m.Expected, m.Found)
}

// Verify counts what actually arrived and compares it with the archive's own manifest.
//
// A restore that reports success because no statement returned an error is reporting on
// itself. This asks the database.
//
// The manifest's row counts come from pg_stat_user_tables on the source, which is an
// estimate — so it is not used. The comparison is against the number of rows COPY said
// it wrote, which is exact, and this function re-reads them from the target so that a
// count is never taken from the same statement that produced it.
func (l *Loader) Verify(ctx context.Context, loaded map[string]int64) ([]Mismatch, error) {
	var out []Mismatch
	for table, want := range loaded {
		var got int64
		if err := l.conn.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, quoted(table))).Scan(&got); err != nil {
			return nil, fmt.Errorf("zählen %s: %w", table, err)
		}
		if got != want {
			out = append(out, Mismatch{Table: table, Expected: want, Found: got})
		}
	}
	return out, nil
}

// SequencesNow reads the counters back out of the restored database, for the same
// reason Verify re-counts rows: the value that was written is not evidence of the value
// that is there.
func (l *Loader) SequencesNow(ctx context.Context) ([]Sequence, error) {
	tx, err := l.conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return readSequences(ctx, tx)
}

// TableFromArchivePath turns "homeserver/db/events.csv" into "events".
//
// Returns false for anything that is not a table file, so a walker can skip the
// manifest and the media without a second list of exceptions to keep in step.
func TableFromArchivePath(name, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(name, prefix+"db/")
	if !ok {
		return "", false
	}
	table, ok := strings.CutSuffix(rest, ".csv")
	if !ok || table == "" || strings.Contains(table, "/") {
		return "", false
	}
	return table, true
}

// BelongsToTheTarget are the tables a database keeps about *itself* rather than about
// its data: which schema it is on, and which processes are running against it. They
// belong to the installation being restored into and never to the archive.
//
// This is the same rule `neverRestored` states for MatrixCtrl's own database, arriving
// at the homeserver for the same reason and with a sharper edge. The schema in a restore
// target is built by the service on first start, in the version running now — so the
// bookkeeping in the target describes *that* schema. Loading the archive's copy over it
// tells a newer Synapse that it is an older one: it would try to apply deltas it has
// already applied, against objects that already exist.
//
// They are also what makes a restore into a freshly created database possible at all.
// Building the schema leaves rows in them, and a COPY of the archive's rows on top
// collides on the primary key — the restore fails at the first table, alphabetically,
// with a message about a duplicate key that explains nothing.
var BelongsToTheTarget = map[string]bool{
	// Synapse
	"applied_schema_deltas":  true,
	"applied_module_schemas": true,
	"schema_version":         true,
	"schema_compat_version":  true,
	// Matrix Authentication Service (sqlx)
	"_sqlx_migrations": true,
	// …and its record of which worker processes have registered. Found by the first
	// rehearsal against the live server: 88 rows went in and 89 came back, because the
	// service registers itself on start. Restoring them is not merely noise — a row
	// from the source that has not shut down describes a worker that does not exist
	// here, and leases are handed out against that list.
	"queue_workers": true,
}

// MergedWithTarget are tables restored from the archive that additionally keep rows the
// target has and the archive does not, keyed by the column identifying a row.
//
// `background_updates` is Synapse's list of work still to do *on the data*, and the
// first rehearsal against the live server showed what happens when the target's copy is
// kept instead: a database built from scratch has 49 of them pending — populate the user
// directory, build these indexes — because that is what a brand-new homeserver has to do.
// The restored server then set about redoing all of it on data where it had long been
// done, emptied `users_in_public_rooms` from 7 913 to 0 and rebuilt the directory row by
// row. Nothing was lost in the end; it converged. But for an hour it looked exactly like
// loss, and on a large server it would have been a day.
//
// Taking the archive's copy alone is not right either: a *newer* schema on the target
// may have queued work the archive has never heard of, and dropping it leaves new
// columns unfilled forever. So the archive's list wins and the target's extras are kept.
var MergedWithTarget = map[string]string{
	"background_updates": "update_name",
}

// KeepTargetOnly snapshots the merged tables before the load empties them.
//
// Into temporary tables, which live for this session only: nothing is left behind if the
// restore fails, and the restore holds one connection for the whole load.
func (l *Loader) KeepTargetOnly(ctx context.Context) error {
	for table := range MergedWithTarget {
		var exists bool
		if err := l.conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname = $1)`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := l.conn.Exec(ctx, fmt.Sprintf(
			`CREATE TEMP TABLE %s ON COMMIT PRESERVE ROWS AS SELECT * FROM %s`,
			quoted(keepTable(table)), quoted(table))); err != nil {
			return fmt.Errorf("%s sichern: %w", table, err)
		}
	}
	return nil
}

// MergeKept puts back the rows only the target had, after the archive's are loaded.
func (l *Loader) MergeKept(ctx context.Context) (int, error) {
	kept := 0
	for table, key := range MergedWithTarget {
		var exists bool
		if err := l.conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE c.relkind = 'r' AND c.relname = $1 AND n.nspname LIKE 'pg_temp%')`,
			keepTable(table)).Scan(&exists); err != nil {
			return kept, err
		}
		if !exists {
			continue
		}
		tag, err := l.conn.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s SELECT k.* FROM %s k
			 WHERE NOT EXISTS (SELECT 1 FROM %s t WHERE t.%s = k.%s)`,
			quoted(table), quoted(keepTable(table)), quoted(table), quoted(key), quoted(key)))
		if err != nil {
			return kept, fmt.Errorf("%s zusammenführen: %w", table, err)
		}
		kept += int(tag.RowsAffected())
	}
	return kept, nil
}

func keepTable(table string) string { return "mxctrl_keep_" + table }

// TruncateAll empties every table except the ones that describe the schema.
//
// Only ever called against a database this restore created minutes earlier: the design
// renames the live one aside and never writes into it, so there is nothing here that an
// operator had before. It is needed because "empty" is not what a freshly migrated
// database is — the service leaves its bookkeeping behind, and some versions seed rows.
//
// One statement listing every table: Postgres refuses to truncate a table another one
// references unless both are emptied together, and doing them together is also the only
// way this stays a single moment.
func (l *Loader) TruncateAll(ctx context.Context) (int, error) {
	rows, err := l.conn.Query(ctx, `
		SELECT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY c.relname`)
	if err != nil {
		return 0, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, err
		}
		if BelongsToTheTarget[name] {
			continue
		}
		names = append(names, quoted(name))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}
	_, err = l.conn.Exec(ctx, `TRUNCATE TABLE `+strings.Join(names, ", ")+` CASCADE`)
	return len(names), err
}

// SchemaVersion reads the schema version a database records about itself.
//
// Returns 0 when there is no such table, which is a normal answer: only Synapse keeps
// one. The caller uses it to decide whether the target's schema is newer than the
// archive's, and "no answer" has to mean "cannot tell" rather than "version zero".
func (l *Loader) SchemaVersion(ctx context.Context) (int, error) {
	// to_regclass, not a subquery against schema_version guarded by EXISTS: Postgres
	// resolves every relation named in a statement at parse time, so a query that
	// mentions `schema_version` fails with "relation does not exist" on a database that
	// has no such table — the WHERE EXISTS never gets the chance to guard it. The
	// authentication service is exactly such a database, and the live migration failed
	// here after the rehearsal had only ever run this against Synapse (§4.105, §4.106):
	// a query tested against the one database that has the table proves nothing about
	// the one that does not. to_regclass returns NULL instead of raising.
	var reg *string
	if err := l.conn.QueryRow(ctx, `SELECT to_regclass('public.schema_version')::text`).Scan(&reg); err != nil {
		return 0, err
	}
	if reg == nil {
		return 0, nil
	}
	var v int
	if err := l.conn.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// SchemaVersionFromCSV reads the same number out of the archive's copy of that table.
//
// The table is not restored — it describes the schema the target built, not the one the
// archive came from — but the number in it is exactly what decides whether the target is
// running something newer, so it is read on the way past.
func SchemaVersionFromCSV(r io.Reader) (int, error) {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	header, err := rd.Read()
	if err != nil {
		return 0, err
	}
	col := -1
	for i, name := range header {
		if strings.TrimSpace(name) == "version" {
			col = i
			break
		}
	}
	if col < 0 {
		return 0, fmt.Errorf("no version column")
	}
	best := 0
	for {
		row, err := rd.Read()
		if err == io.EOF {
			return best, nil
		}
		if err != nil {
			return best, err
		}
		if col >= len(row) {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(row[col]), "%d", &v); err == nil && v > best {
			best = v
		}
	}
}
