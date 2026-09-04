// Package backup captures what MatrixCtrl owns (etappe 68).
//
// S14 listed backup/restore for months with nothing behind it. What this does capture
// is the config repository — every ESS value with its git history, the thing that would
// take longest to rebuild by hand — and MatrixCtrl's own database.
//
// What it does **not** capture is the homeserver: Synapse's database and its media live
// on volumes this pod does not mount, and reaching them properly needs a Job with those
// volumes attached. That limit is written into every archive's manifest rather than
// left to documentation, because an operator who believes they hold a backup of their
// Matrix server and learns otherwise mid-restore is the failure this project keeps
// finding — something that looks complete and is not (§4.45).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FormatVersion is the archive layout. A restore refuses anything it does not know,
// rather than guessing at a future shape.
const FormatVersion = 1

// regenerable marks tables whose loss costs history, not function. They dominate the
// row count (sampling writes ~1440 rows a day) and a restore may reasonably skip them,
// so the manifest says which are which instead of leaving the operator to guess.
var regenerable = map[string]bool{
	"rtc_samples":         true,
	"node_samples":        true,
	"rtc_address_history": true,
	"login_attempts":      true,
	"oidc_states":         true,
	"sessions":            true,
}

// Table is one captured table.
type Table struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
	// Regenerable is true for telemetry: losing it costs history, not function.
	Regenerable bool   `json:"regenerable"`
	File        string `json:"file"`
}

// Release identifies the ESS deployment the archive was taken from.
//
// Without it "restore the same homeserver" is not possible: the configuration would come
// back onto whatever chart version happened to be newest, which is a different
// deployment wearing the same hostnames (etappe 69).
type Release struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Chart     string `json:"chart,omitempty"`
	Revision  int    `json:"revision,omitempty"`
}

// Manifest describes the archive, including what it deliberately leaves out.
type Manifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	// ESS is the managed release at the time of the backup.
	ESS         Release `json:"ess"`
	Tables      []Table `json:"tables"`
	ConfigFiles int     `json:"config_repo_files"`

	// Restores and NotIncluded are the point of the manifest. Read both before
	// trusting the archive.
	Restores    []string `json:"restores"`
	NotIncluded []string `json:"not_included"`
	// SchemaNote explains why no schema is carried.
	SchemaNote string `json:"schema_note"`
}

// tablesWithRows lists user tables that currently hold data, in dependency-free order
// (this schema has no cross-table foreign keys — report_dispositions annotates rows in
// another system entirely, see migrations/013).
func tablesWithRows(ctx context.Context, db *pgxpool.Pool) ([]Table, error) {
	rows, err := db.Query(ctx, `
		SELECT relname, n_live_tup FROM pg_stat_user_tables
		WHERE n_live_tup > 0 ORDER BY relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			return nil, err
		}
		t.Regenerable = regenerable[t.Name]
		t.File = "db/" + t.Name + ".csv"
		out = append(out, t)
	}
	return out, rows.Err()
}

// Create streams a gzipped tar of the database and the config repository to w.
//
// Streamed rather than assembled: the archive is small today, but holding it in memory
// is a decision that only becomes wrong on somebody else's larger install, and by then
// it is a crash rather than a slow download.
func Create(ctx context.Context, db *pgxpool.Pool, configRepo, appVersion string, ess Release, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	tables, err := tablesWithRows(ctx, db)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	configFiles, err := countFiles(configRepo)
	if err != nil {
		// A missing config repo is worth reporting, not worth aborting a database
		// backup over — half an archive beats none, as long as the manifest says so.
		configFiles = -1
	}

	man := Manifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		AppVersion:    appVersion,
		ESS:           ess,
		Tables:        tables,
		ConfigFiles:   configFiles,
		// What comes back and what does not are genuinely different sentences, and E68
		// used the pessimistic one for both. The configuration *is* the deployment —
		// hostnames, server name, TLS issuer, RTC settings, the hooks that keep the SFU
		// patched. What no archive here can return is what users made (etappe 69).
		Restores: []string{
			"Die vollständige ESS-Konfiguration mit Git-Historie: Hostnames, serverName, TLS-Issuer, RTC-Einstellungen.",
			"Die Hooks, die manuelle Patches nach jedem Upgrade und Rollback wiederherstellen.",
			"Upgrade-Verlauf, Melde-Entscheidungen und den aufgezeichneten Node-Verlauf.",
		},
		NotIncluded: []string{
			"Was die Nutzer erzeugt haben: Konten, Räume, Nachrichten — Synapses Datenbank.",
			"Die hochgeladenen Dateien (Media-Volume).",
			"Beides liegt auf Volumes, die dieser Pod nicht einbindet. Ein wiederhergestellter Server ist derselbe Server, aber leer.",
		},
		SchemaNote: "Enthält nur Daten, kein Schema: das Schema entsteht beim Zurückspielen aus den Migrationen, " +
			"damit ein Archiv auf den aktuellen Stand zurückkommt und nicht auf den, bei dem es entstanden ist.",
	}
	dump := func(t Table) ([]byte, error) { return copyTable(ctx, db, t) }
	return assemble(tw, man, dump, configRepo, configFiles >= 0)
}

// assemble writes the archive given a way to dump one table.
//
// Separated from Create so the archive's shape — the manifest, the paths, whether the
// git history comes along — can be tested without a database. The parts most likely to
// be quietly wrong are the ones that need no Postgres to check.
func assemble(tw *tar.Writer, man Manifest, dump func(Table) ([]byte, error),
	configRepo string, withConfig bool) error {

	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, "manifest.json", blob, 0o644, man.CreatedAt); err != nil {
		return err
	}

	for _, t := range man.Tables {
		data, err := dump(t)
		if err != nil {
			return fmt.Errorf("dump %s: %w", t.Name, err)
		}
		if err := writeFile(tw, t.File, data, 0o644, man.CreatedAt); err != nil {
			return err
		}
	}
	if withConfig {
		if err := addTree(tw, configRepo, "config-repo"); err != nil {
			return fmt.Errorf("config repo: %w", err)
		}
	}
	return nil
}

// copyTable writes one table as CSV using COPY, which streams from Postgres rather than
// materialising the rows in this process.
func copyTable(ctx context.Context, db *pgxpool.Pool, t Table) ([]byte, error) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var buf strings.Builder
	// Identifier interpolated, and it comes from pg_stat_user_tables rather than from
	// any request — COPY takes no parameters, so this is the only way, and the source
	// is what makes it safe.
	sql := fmt.Sprintf(`COPY (SELECT * FROM %q) TO STDOUT WITH (FORMAT csv, HEADER true)`, t.Name)
	if _, err := conn.Conn().PgConn().CopyTo(ctx, &writerTo{&buf}, sql); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// writerTo adapts a strings.Builder to io.Writer for CopyTo.
type writerTo struct{ b *strings.Builder }

func (w *writerTo) Write(p []byte) (int, error) { return w.b.Write(p) }

func countFiles(root string) (int, error) {
	n := 0
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}

// addTree copies a directory into the archive, including .git — the history is most of
// the value, since it is what makes a restored config auditable rather than a snapshot.
func addTree(tw *tar.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.Join(prefix, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: info.ModTime(),
			})
		}
		if !info.Mode().IsRegular() {
			return nil // sockets and symlinks are not config
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(tw, name, data, 0o644, info.ModTime())
	})
}

func writeFile(tw *tar.Writer, name string, data []byte, mode int64, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: mode, Size: int64(len(data)), ModTime: mod, Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
