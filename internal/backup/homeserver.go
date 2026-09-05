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
	Snapshot    string   `json:"snapshot"`
	Tables      []Table  `json:"tables"`
	NotIncluded []string `json:"not_included"`
	RestoreNote string   `json:"restore_note"`
}

// ExportHomeserver streams Synapse's database as a consistent snapshot.
//
// Everything happens inside one REPEATABLE READ transaction. Reading a live database
// table by table otherwise yields tables from different moments — a room that exists
// with no creation event — and the result looks complete while being subtly torn, which
// is this project's recurring failure in a new place.
func ExportHomeserver(ctx context.Context, conn *pgx.Conn, dbName string, w io.Writer) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // read-only; nothing to commit

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

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	man := HomeserverManifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		Database:      dbName,
		Snapshot:      "Alle Tabellen stammen aus einer einzigen REPEATABLE-READ-Transaktion, also aus demselben Moment.",
		Tables:        tables,
		NotIncluded: []string{
			"Die hochgeladenen Dateien (Media-Volume) — die liegen auf einem Volume, das nur der Synapse-Pod einbindet.",
			"Die Konfiguration des Servers — die steckt im separaten MatrixCtrl-Backup.",
		},
		RestoreNote: homeserverRestoreNote,
	}
	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, "manifest.json", blob, 0o644, man.CreatedAt); err != nil {
		return err
	}

	for _, t := range tables {
		var buf strings.Builder
		sql := fmt.Sprintf(`COPY (SELECT * FROM %q) TO STDOUT WITH (FORMAT csv, HEADER true)`, t.Name)
		if _, err := tx.Conn().PgConn().CopyTo(ctx, &writerTo{&buf}, sql); err != nil {
			return fmt.Errorf("dump %s: %w", t.Name, err)
		}
		if err := writeFile(tw, t.File, []byte(buf.String()), 0o644, man.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}
