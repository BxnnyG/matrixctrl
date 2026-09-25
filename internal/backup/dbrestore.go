package backup

import (
	"context"
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
