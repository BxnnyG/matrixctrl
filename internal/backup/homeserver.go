package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Exporting the homeserver's own database (etappe 70).
//
// E68's archive rebuilds the *deployment* — hostnames, TLS, RTC, hooks. This is the part
// that makes a restored server the same server rather than a fresh one wearing the same
// hostnames: 19 057 events, the accounts and the rooms, measured on the live install.
//
// Synapse's media is **not** here. It is 40 MB on a volume only the Synapse pod mounts,
// and reaching it needs a Job with that PVC attached — a different mechanism, recorded
// rather than half-built. The manifest says so.

// HomeserverManifest describes a database export.
type HomeserverManifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	Database      string    `json:"database"`
	// Snapshot records that every table came from one moment, which is the difference
	// between a backup and a pile of unrelated reads.
	Snapshot string  `json:"snapshot"`
	Tables   []Table `json:"tables"`
	// Sequences are the counters that belong to the data without living in any table.
	// Absent in format 1, which is why an archive from before etappe 106 cannot be
	// restored as the same server — see Sequence.
	Sequences   []Sequence `json:"sequences,omitempty"`
	NotIncluded []string   `json:"not_included"`
	RestoreNote string     `json:"restore_note"`
}

// Sequence is a counter Postgres hands out numbers from — and the part of a database
// that a table-by-table dump silently leaves behind.
//
// Synapse draws `stream_ordering`, `state_group` ids and device-list positions from 25
// of them. Restore the rows without the counters and they start at 1 again while the
// restored rows already occupy up to 47 485: new events either collide on a primary key
// (visible) or sort *before* the entire history (not visible — the rooms simply look
// frozen), and two rooms can end up sharing a state group. A restore that does that
// looks like a successful migration until it does not.
//
// LastValue is nil when the sequence has never been read from. That is not the same as
// zero, and `setval` treats them differently, so it is carried as a distinct state
// rather than flattened.
type Sequence struct {
	Name      string `json:"name"`
	LastValue *int64 `json:"last_value"`
}

// ExportHomeserver streams one database as a consistent snapshot, on its own.
//
// It is the same export the combined archive writes, at the root of its own file rather
// than under a prefix — one implementation, because two of them is how the combined
// archive ended up without the accounts (§4.102) and how sequences would have been
// added to one exporter and not the other.
func ExportHomeserver(ctx context.Context, conn *pgx.Conn, dbName string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return exportDatabaseUnder(ctx, tw, "", conn, dbName, time.Now().UTC(), []string{
		"Die hochgeladenen Dateien (Media-Volume) — die liegen auf einem Volume, das nur der Synapse-Pod einbindet.",
		"Die Konfiguration des Servers — die steckt im separaten MatrixCtrl-Backup.",
	})
}

// readSequences reads every counter in the schema.
//
// Sequences are **not** transactional: this read sees the value at the moment it runs,
// not the value at the snapshot the tables come from. That is the safe direction — a
// counter slightly ahead of the data hands out numbers nobody used, while one behind
// hands out numbers that are already taken. It is worth saying out loud because the
// surrounding function goes to some trouble for a consistent snapshot, and this one
// line deliberately is not part of it.
//
// `last_value` is NULL for a sequence that has never been read from, and NULL is also
// what a caller without privileges on the sequence sees. Here the exporting role owns
// them, which is a measurement (`pg_class.relowner`), not an assumption — and the live
// test asserts that the used counters come back non-nil, because a silent column of
// nulls would produce an archive that restores to a broken server (§4.105, §4.106).
func readSequences(ctx context.Context, tx pgx.Tx) ([]Sequence, error) {
	rows, err := tx.Query(ctx, `
		SELECT sequencename, last_value
		FROM pg_sequences
		WHERE schemaname = 'public'
		ORDER BY sequencename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sequence
	for rows.Next() {
		var s Sequence
		if err := rows.Scan(&s.Name, &s.LastValue); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// exportDatabaseUnder is ExportHomeserver writing into an existing archive.
//
// Still one REPEATABLE READ transaction: a homeserver read table by table while running
// yields tables from different moments, and being inside a larger archive changes
// nothing about that (§4.69).
func exportDatabaseUnder(ctx context.Context, tw *tar.Writer, prefix string,
	conn *pgx.Conn, dbName string, at time.Time, notIncluded []string) error {

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT c.relname, COALESCE(s.n_live_tup, 0)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY c.relname`)
	if err != nil {
		return err
	}
	var tables []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			rows.Close()
			return err
		}
		t.File = "db/" + t.Name + ".csv"
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	seqs, err := readSequences(ctx, tx)
	if err != nil {
		return fmt.Errorf("sequences: %w", err)
	}

	man := HomeserverManifest{
		FormatVersion: FormatVersion,
		CreatedAt:     at,
		Database:      dbName,
		Sequences:     seqs,
		Snapshot:      "Alle Tabellen stammen aus einer einzigen REPEATABLE-READ-Transaktion, also aus demselben Moment.",
		Tables:        tables,
		NotIncluded:   notIncluded,
		RestoreNote:   homeserverRestoreNote,
	}
	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, prefix+"manifest.json", blob, 0o644, at); err != nil {
		return err
	}

	for _, t := range tables {
		var buf strings.Builder
		sql := fmt.Sprintf(`COPY (SELECT * FROM %q) TO STDOUT WITH (FORMAT csv, HEADER true)`, t.Name)
		if _, err := tx.Conn().PgConn().CopyTo(ctx, &writerTo{&buf}, sql); err != nil {
			return fmt.Errorf("dump %s: %w", t.Name, err)
		}
		if err := writeFile(tw, prefix+t.File, []byte(buf.String()), 0o644, at); err != nil {
			return err
		}
	}
	return nil
}
