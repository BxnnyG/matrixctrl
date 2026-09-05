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
	"github.com/jackc/pgx/v5/pgxpool"
)

// One archive instead of two (etappe 72).
//
// E68 and E69 produced a configuration archive; E70 added a separate homeserver dump.
// Both were right on their own and wrong together: the Backup page showed the order the
// features were built in rather than the operator's task, with three warning blocks
// explaining what each half could not do.
//
// "the backup" should be one thing you can hold.

// FullManifest covers every part in a combined archive.
type FullManifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	ESS           Release   `json:"ess"`
	// Parts is what is inside, in the order it was written.
	Parts []string `json:"parts"`
	// Config and Homeserver are the two sub-manifests, kept whole so each part can still
	// be read on its own terms.
	Config     Manifest            `json:"config"`
	Homeserver *HomeserverManifest `json:"homeserver,omitempty"`
	// NotIncluded is now one line rather than three warnings on a screen.
	NotIncluded []string `json:"not_included"`
}

// CreateFull writes configuration, MatrixCtrl's database and Synapse's database into a
// single archive.
//
// The homeserver connection is optional: without cluster access there is no way to reach
// Synapse's database, and half an archive with a manifest that says so beats refusing to
// produce one at all.
func CreateFull(ctx context.Context, db *pgxpool.Pool, hs *pgx.Conn,
	configRepo, appVersion string, ess Release, w io.Writer) error {

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	tables, err := tablesWithRows(ctx, db)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	configFiles, ferr := countFiles(configRepo)
	if ferr != nil {
		configFiles = -1
	}

	cfgMan := Manifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		AppVersion:    appVersion,
		ESS:           ess,
		Tables:        tables,
		ConfigFiles:   configFiles,
		Restores:      configRestores,
		SchemaNote:    schemaNote,
	}

	full := FullManifest{
		FormatVersion: FormatVersion,
		CreatedAt:     cfgMan.CreatedAt,
		AppVersion:    appVersion,
		ESS:           ess,
		Parts:         []string{"matrixctrl/"},
		Config:        cfgMan,
		NotIncluded: []string{
			"Die hochgeladenen Dateien (Media-Volume) — die liegen auf einem Volume, das nur der Synapse-Pod einbindet.",
		},
	}
	if hs != nil {
		full.Parts = append(full.Parts, "homeserver/")
	} else {
		full.NotIncluded = append(full.NotIncluded,
			"Synapses Datenbank — dieser Lauf hatte keinen Zugriff darauf.")
	}

	// The combined manifest first, so anything reading the stream knows what is coming.
	blob, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, "manifest.json", blob, 0o644, full.CreatedAt); err != nil {
		return err
	}

	// Part one: what MatrixCtrl owns. Prefixed, so each part stays readable on its own
	// and a restore can tell them apart without guessing from table names.
	dump := func(t Table) ([]byte, error) { return copyTable(ctx, db, t) }
	if err := assembleUnder(tw, "matrixctrl/", cfgMan, dump, configRepo, configFiles >= 0); err != nil {
		return err
	}

	// Part two: the homeserver itself.
	if hs != nil {
		if err := exportHomeserverUnder(ctx, tw, "homeserver/", hs, "synapse", full.CreatedAt); err != nil {
			return fmt.Errorf("homeserver: %w", err)
		}
	}
	return nil
}

// configRestores and schemaNote are shared with Create so the two archives describe the
// same contents in the same words (rule 3).
var configRestores = []string{
	"Die vollständige ESS-Konfiguration mit Git-Historie: Hostnames, serverName, TLS-Issuer, RTC-Einstellungen.",
	"Die Hooks, die manuelle Patches nach jedem Upgrade und Rollback wiederherstellen.",
	"Upgrade-Verlauf, Melde-Entscheidungen und den aufgezeichneten Node-Verlauf.",
}

const schemaNote = "Enthält nur Daten, kein Schema: das Schema entsteht beim Zurückspielen aus den Migrationen, " +
	"damit ein Archiv auf den aktuellen Stand zurückkommt und nicht auf den, bei dem es entstanden ist."

// assembleUnder is assemble with every path prefixed.
func assembleUnder(tw *tar.Writer, prefix string, man Manifest,
	dump func(Table) ([]byte, error), configRepo string, withConfig bool) error {

	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, prefix+"manifest.json", blob, 0o644, man.CreatedAt); err != nil {
		return err
	}
	for _, t := range man.Tables {
		data, err := dump(t)
		if err != nil {
			return fmt.Errorf("dump %s: %w", t.Name, err)
		}
		if err := writeFile(tw, prefix+t.File, data, 0o644, man.CreatedAt); err != nil {
			return err
		}
	}
	if withConfig {
		return addTree(tw, configRepo, prefix+"config-repo")
	}
	return nil
}

// exportHomeserverUnder is ExportHomeserver writing into an existing archive.
//
// Still one REPEATABLE READ transaction: a homeserver read table by table while running
// yields tables from different moments, and being inside a larger archive changes
// nothing about that (§4.69).
func exportHomeserverUnder(ctx context.Context, tw *tar.Writer, prefix string,
	conn *pgx.Conn, dbName string, at time.Time) error {

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

	man := HomeserverManifest{
		FormatVersion: FormatVersion,
		CreatedAt:     at,
		Database:      dbName,
		Snapshot:      "Alle Tabellen stammen aus einer einzigen REPEATABLE-READ-Transaktion, also aus demselben Moment.",
		Tables:        tables,
		NotIncluded:   []string{"Die hochgeladenen Dateien (Media-Volume)."},
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

const homeserverRestoreNote = "Zurückspielen ist bewusst kein Knopf: dafür muss Synapse gestoppt sein, " +
	"und es im laufenden Betrieb zu tun beschädigt, was da ist. Das Archiv ist eine " +
	"Datei, die bewusst mit psql eingespielt wird."
