package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Restoring an archive (etappe 69).
//
// Taking a backup is harmless; writing one back destroys what is there, which is why it
// arrived one etappe later than Create and with the guards below.

// neverRestored are tables whose contents belong to the live database rather than to any
// archive.
//
// schema_migrations is the database's bookkeeping about itself. Overwriting it with a
// backup's copy would tell the application that migrations it has already run are
// pending — or that pending ones are done, which is worse.
var neverRestored = map[string]bool{"schema_migrations": true}

// Archive is a read but not yet applied backup.
type Archive struct {
	Manifest Manifest
	tables   map[string][]byte // table name -> CSV
	config   map[string][]byte // path under config-repo/ -> contents
}

// Read parses an archive without changing anything, so the operator can be shown what
// they are about to restore before it happens.
func Read(r io.Reader) (*Archive, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("kein gültiges gzip-Archiv: %w", err)
	}
	defer gz.Close()

	a := &Archive{tables: map[string][]byte{}, config: map[string][]byte{}}
	tr := tar.NewReader(gz)
	seenManifest := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("beschädigtes Archiv: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		// Refuse anything that would escape its directory. A tar can contain "../"
		// and this one is uploaded by a browser.
		clean := filepath.Clean("/" + h.Name)[1:]
		data, err := io.ReadAll(io.LimitReader(tr, 256<<20))
		if err != nil {
			return nil, err
		}
		switch {
		case clean == "manifest.json":
			if err := json.Unmarshal(data, &a.Manifest); err != nil {
				return nil, fmt.Errorf("Manifest unlesbar: %w", err)
			}
			seenManifest = true
		case strings.HasPrefix(clean, "db/") && strings.HasSuffix(clean, ".csv"):
			a.tables[strings.TrimSuffix(strings.TrimPrefix(clean, "db/"), ".csv")] = data
		case strings.HasPrefix(clean, "config-repo/"):
			a.config[strings.TrimPrefix(clean, "config-repo/")] = data
		}
	}
	if !seenManifest {
		return nil, fmt.Errorf("kein Manifest im Archiv — das ist kein MatrixCtrl-Backup")
	}
	// An unknown layout is refused rather than guessed at: a newer archive may mean
	// something different by the same file names.
	if a.Manifest.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("Archivformat %d, dieses MatrixCtrl versteht %d",
			a.Manifest.FormatVersion, FormatVersion)
	}
	return a, nil
}

// RestoreDatabase writes the archive's tables back, in one transaction.
//
// Columns are matched by name against the *current* schema: a column since dropped is
// skipped, one since added takes its default. That is what makes an archive taken before
// a migration restorable after it, and it is the entire reason no schema is carried
// (§4.66).
func (a *Archive) RestoreDatabase(ctx context.Context, db *pgxpool.Pool) (restored []string, err error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// A failure part-way must leave the database as it was, not half-replaced.
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, t := range a.Manifest.Tables {
		if neverRestored[t.Name] {
			continue
		}
		data, ok := a.tables[t.Name]
		if !ok {
			continue // listed but absent: nothing to write, and not a reason to abort
		}
		n, rerr := restoreTable(ctx, tx, t.Name, data)
		if rerr != nil {
			err = fmt.Errorf("%s: %w", t.Name, rerr)
			return nil, err
		}
		restored = append(restored, fmt.Sprintf("%s (%d)", t.Name, n))
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return restored, nil
}

func restoreTable(ctx context.Context, tx pgx.Tx, table string, csvData []byte) (int, error) {
	rows, err := csv.NewReader(bytes.NewReader(csvData)).ReadAll()
	if err != nil {
		return 0, fmt.Errorf("CSV unlesbar: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	header := rows[0]

	// Which of the archive's columns still exist here.
	live := map[string]bool{}
	cur, err := tx.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1`, table)
	if err != nil {
		return 0, err
	}
	for cur.Next() {
		var c string
		if err := cur.Scan(&c); err != nil {
			cur.Close()
			return 0, err
		}
		live[c] = true
	}
	cur.Close()
	if len(live) == 0 {
		// The table itself is gone — an archive from before it was dropped. Skipping is
		// right: restoring it would recreate a table the schema deliberately removed.
		return 0, nil
	}

	var cols []string
	var idx []int
	for i, name := range header {
		if live[name] {
			cols = append(cols, name)
			idx = append(idx, i)
		}
	}
	if len(cols) == 0 {
		return 0, nil
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE %q", table)); err != nil {
		return 0, err
	}

	src := pgx.CopyFromSlice(len(rows)-1, func(i int) ([]any, error) {
		rec := rows[i+1]
		out := make([]any, len(idx))
		for j, at := range idx {
			if at >= len(rec) {
				out[j] = nil
				continue
			}
			// COPY CSV writes NULL as an empty unquoted field; encoding/csv cannot tell
			// that from an empty string, so empty becomes NULL. Every column here is
			// either nullable or has a default, so this is the safe direction.
			if rec[at] == "" {
				out[j] = nil
				continue
			}
			out[j] = rec[at]
		}
		return out, nil
	})
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, src)
	return int(n), err
}

// RestoreConfigRepo replaces the config repository with the archive's copy.
//
// The whole directory including .git, so the restored configuration keeps its history
// rather than becoming a snapshot with no past.
func (a *Archive) RestoreConfigRepo(root string) (int, error) {
	if len(a.config) == 0 {
		return 0, nil
	}
	// Written beside the live one and swapped, so a failure halfway does not leave a
	// half-written config repository behind.
	tmp := root + ".restoring"
	if err := os.RemoveAll(tmp); err != nil {
		return 0, err
	}
	n := 0
	for rel, data := range a.config {
		dst := filepath.Join(tmp, filepath.Clean("/" + rel)[1:])
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return 0, err
		}
		n++
	}
	old := root + ".replaced"
	_ = os.RemoveAll(old)
	if err := os.Rename(root, old); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if err := os.Rename(tmp, root); err != nil {
		_ = os.Rename(old, root) // put the original back
		return 0, err
	}
	_ = os.RemoveAll(old)
	return n, nil
}
