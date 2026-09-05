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
	"sort"
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
	// A full archive (etappe 72) puts MatrixCtrl's part under matrixctrl/ and Synapse's
	// under homeserver/. Both layouts are accepted: archives in the flat one already
	// exist, and a reader that silently ignores half of a newer file is exactly what
	// this function did before etappe 73 — no error, nothing restored, success reported.
	const nested = "matrixctrl/"
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
		// Everything below matrixctrl/ is read as if it were at the root; homeserver/
		// is deliberately skipped, because Synapse's tables belong to another database
		// and are restored with psql, not written into this one.
		inner := clean
		if strings.HasPrefix(clean, nested) {
			inner = strings.TrimPrefix(clean, nested)
		} else if strings.HasPrefix(clean, "homeserver/") {
			continue
		}

		switch {
		case clean == "manifest.json":
			// The root manifest of a full archive is a FullManifest; its nested Config
			// field carries what a flat archive keeps at the top. Decoded into both so
			// either layout produces the same Manifest.
			var full FullManifest
			if json.Unmarshal(data, &full) == nil && len(full.Parts) > 0 {
				a.Manifest = full.Config
				a.Manifest.FormatVersion = full.FormatVersion
				a.Manifest.ESS = full.ESS
				a.Manifest.CreatedAt = full.CreatedAt
				a.Manifest.AppVersion = full.AppVersion
				a.Manifest.NotIncluded = full.NotIncluded
			} else if err := json.Unmarshal(data, &a.Manifest); err != nil {
				return nil, fmt.Errorf("Manifest unlesbar: %w", err)
			}
			seenManifest = true
		case inner == "manifest.json":
			// The nested manifest is authoritative for the table list.
			var m Manifest
			if json.Unmarshal(data, &m) == nil && len(m.Tables) > 0 {
				a.Manifest.Tables = m.Tables
				a.Manifest.ConfigFiles = m.ConfigFiles
			}
		case strings.HasPrefix(inner, "db/") && strings.HasSuffix(inner, ".csv"):
			a.tables[strings.TrimSuffix(strings.TrimPrefix(inner, "db/"), ".csv")] = data
		case strings.HasPrefix(inner, "config-repo/"):
			a.config[strings.TrimPrefix(inner, "config-repo/")] = data
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

	var want []string
	for _, t := range a.Manifest.Tables {
		if neverRestored[t.Name] {
			continue
		}
		if _, ok := a.tables[t.Name]; !ok {
			continue // listed but absent: nothing to write, and not a reason to abort
		}
		want = append(want, t.Name)
	}

	// Order comes from the target schema, never from the archive or the alphabet. The
	// target is what the rows have to satisfy, and an archive can predate any constraint
	// in it (etappe 74).
	order, clear, err := restorePlan(ctx, tx, want)
	if err != nil {
		return nil, err
	}

	// One TRUNCATE for all of them, not one per table. Postgres refuses to empty a table
	// another table references — even when that other table holds no rows — unless both
	// are named in the same statement. Per-table TRUNCATE therefore cannot be made to
	// work in any order, and CASCADE would silently empty tables the archive says nothing
	// about. `clear` is the archive's tables plus everything that references them.
	if len(clear) > 0 {
		quoted := make([]string, len(clear))
		for i, name := range clear {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		if _, err = tx.Exec(ctx, "TRUNCATE "+strings.Join(quoted, ", ")); err != nil {
			err = fmt.Errorf("leeren: %w", err)
			return nil, err
		}
	}

	for _, name := range order {
		n, rerr := restoreTable(ctx, tx, name, a.tables[name])
		if rerr != nil {
			err = fmt.Errorf("%s: %w", name, rerr)
			return nil, err
		}
		restored = append(restored, fmt.Sprintf("%s (%d)", name, n))
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return restored, nil
}

// restorePlan returns the tables to fill, every parent ahead of its children, and the
// tables to empty first.
//
// Read from pg_constraint rather than written down here, so a foreign key added by a
// future migration plans itself without anyone remembering this function exists. That is
// the whole point: the list that was written down by hand said there were no foreign keys
// at all, while two had been in the schema since migration 002 (etappe 74).
//
// The set to empty is wider than the set to fill, and deliberately so. A table that holds
// no rows at backup time is not in the archive, but it can still reference one that is,
// and Postgres will not empty a referenced table unless its referencing tables are
// emptied in the same breath. Following the references outward — and no further — keeps
// tables the archive never claimed to cover untouched, which TRUNCATE ... CASCADE would
// not.
func restorePlan(ctx context.Context, tx pgx.Tx, want []string) (order, clear []string, err error) {
	live := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE'`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		live[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// A table the archive knows and this schema does not is skipped, not an error: it is
	// an archive from before the table was dropped, and recreating it would undo a
	// migration.
	pending := map[string]bool{}
	var names []string
	for _, n := range want {
		if live[n] && !pending[n] {
			pending[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)

	// parents[child] orders the fill; children[parent] widens what has to be emptied.
	parents := map[string]map[string]bool{}
	children := map[string][]string{}
	fk, err := tx.Query(ctx, `
		SELECT child.relname, parent.relname
		FROM pg_constraint c
		JOIN pg_class child ON child.oid = c.conrelid
		JOIN pg_class parent ON parent.oid = c.confrelid
		JOIN pg_namespace n ON n.oid = child.relnamespace
		WHERE c.contype = 'f' AND n.nspname = 'public'`)
	if err != nil {
		return nil, nil, err
	}
	for fk.Next() {
		var child, parent string
		if err := fk.Scan(&child, &parent); err != nil {
			fk.Close()
			return nil, nil, err
		}
		// A self-reference is satisfied inside the table's own COPY, and treating it as a
		// dependency would make every such table look like a cycle.
		if child == parent || !live[child] || !live[parent] {
			continue
		}
		children[parent] = append(children[parent], child)
		if pending[child] && pending[parent] {
			if parents[child] == nil {
				parents[child] = map[string]bool{}
			}
			parents[child][parent] = true
		}
	}
	fk.Close()
	if err := fk.Err(); err != nil {
		return nil, nil, err
	}

	seen := map[string]bool{}
	queue := append([]string(nil), names...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if seen[n] || neverRestored[n] {
			continue
		}
		seen[n] = true
		clear = append(clear, n)
		queue = append(queue, children[n]...)
	}
	sort.Strings(clear)

	return topoSort(names, parents), clear, nil
}

// topoSort puts every parent ahead of its children, keeping name order among tables that
// do not constrain each other so two runs of the same restore behave the same way.
//
// A cycle cannot be ordered, and refusing to restore because of one would be a worse
// answer than trying: the remaining tables are appended in name order and Postgres gets
// to say whether it actually minds.
func topoSort(names []string, parents map[string]map[string]bool) []string {
	done := map[string]bool{}
	out := make([]string, 0, len(names))
	for len(out) < len(names) {
		progress := false
		for _, n := range names {
			if done[n] {
				continue
			}
			ready := true
			for p := range parents[n] {
				if !done[p] {
					ready = false
					break
				}
			}
			if ready {
				done[n] = true
				out = append(out, n)
				progress = true
			}
		}
		if !progress {
			for _, n := range names {
				if !done[n] {
					done[n] = true
					out = append(out, n)
				}
			}
		}
	}
	return out
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

	// Which of the archive's columns still exist here, and which of them accept NULL.
	// Nullability is read rather than assumed: see the empty-field handling below.
	live := map[string]bool{}
	nullable := map[string]bool{}
	cur, err := tx.Query(ctx, `SELECT column_name, is_nullable FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1`, table)
	if err != nil {
		return 0, err
	}
	for cur.Next() {
		var c, isNullable string
		if err := cur.Scan(&c, &isNullable); err != nil {
			cur.Close()
			return 0, err
		}
		live[c] = true
		nullable[c] = isNullable == "YES"
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

	src := pgx.CopyFromSlice(len(rows)-1, func(i int) ([]any, error) {
		rec := rows[i+1]
		out := make([]any, len(idx))
		for j, at := range idx {
			if at >= len(rec) {
				out[j] = nil
				continue
			}
			// COPY CSV writes NULL as an empty unquoted field, and encoding/csv cannot
			// tell that from a quoted empty string — so the archive alone cannot say
			// which one a blank was. The schema can: a blank goes back as NULL only
			// where NULL is allowed, and as "" everywhere else.
			//
			// This used to send NULL unconditionally, justified by the claim that every
			// column was "either nullable or has a default". report_dispositions.note is
			// `NOT NULL DEFAULT ''` — and a default does not apply to an explicitly
			// supplied NULL, only to an omitted column. So the reasoning was wrong in a
			// way that made the wrong answer look considered (etappe 74).
			if rec[at] == "" {
				if nullable[header[at]] {
					out[j] = nil
				} else {
					out[j] = ""
				}
				continue
			}
			out[j] = rec[at]
		}
		return out, nil
	})
	n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, src)
	return int(n), err
}

// Staging directories for RestoreConfigRepo, both inside the repository itself.
//
// They have to be inside it. `root` is a mount point: in the pod the config PVC is
// mounted at /data/config-repo and everything above it is the container's read-only
// root filesystem (`readOnlyRootFilesystem: true`). The first version of this
// function staged at root+".restoring" and swapped by renaming root itself, which
// is two impossibilities in one: nothing can be created next to the mount point,
// and a mount point cannot be renamed. It failed for the operator with
//
//	unlinkat /data/config-repo.restoring: read-only file system
//
// and could never have worked anywhere it actually runs. The tests missed it
// because every one of them passed a fresh t.TempDir(), whose parent is writable —
// the same shape of blind spot as etappe 74: a fixture that builds the one world in
// which the defect cannot appear.
const (
	restoreStaging = ".matrixctrl-restore-new"
	restoreOld     = ".matrixctrl-restore-old"
)

// RestoreConfigRepo replaces the config repository with the archive's copy.
//
// The whole directory including .git, so the restored configuration keeps its history
// rather than becoming a snapshot with no past.
//
// The directory identified by root is emptied and refilled; it is never replaced. Its
// contents are moved aside first and put back if the refill fails, so an interrupted
// restore leaves the previous configuration rather than half of the new one.
func (a *Archive) RestoreConfigRepo(root string) (int, error) {
	if len(a.config) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, err
	}
	staging := filepath.Join(root, restoreStaging)
	old := filepath.Join(root, restoreOld)
	for _, dir := range []string{staging, old} {
		if err := os.RemoveAll(dir); err != nil {
			return 0, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	defer func() {
		_ = os.RemoveAll(staging)
		_ = os.RemoveAll(old)
	}()

	n := 0
	for rel, data := range a.config {
		clean := filepath.Clean("/" + rel)[1:]
		// An archive that carried a path of our own staging names would have its
		// files swept away by the move below, silently restoring less than it says.
		if top, _, _ := strings.Cut(clean, string(filepath.Separator)); top == restoreStaging || top == restoreOld {
			return 0, fmt.Errorf("archive contains a reserved path: %s", rel)
		}
		dst := filepath.Join(staging, clean)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return 0, err
		}
		n++
	}

	// Move what is there aside. Same directory, so these are renames, not copies.
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	moved := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name() == restoreStaging || e.Name() == restoreOld {
			continue
		}
		if err := os.Rename(filepath.Join(root, e.Name()), filepath.Join(old, e.Name())); err != nil {
			restoreBack(old, root, moved)
			return 0, err
		}
		moved = append(moved, e.Name())
	}

	// Put the archive's copy in place. On failure the previous contents go back, so
	// the operator is left where they started instead of with an empty repository.
	staged, err := os.ReadDir(staging)
	if err != nil {
		restoreBack(old, root, moved)
		return 0, err
	}
	placed := make([]string, 0, len(staged))
	for _, e := range staged {
		if err := os.Rename(filepath.Join(staging, e.Name()), filepath.Join(root, e.Name())); err != nil {
			for _, name := range placed {
				_ = os.Rename(filepath.Join(root, name), filepath.Join(staging, name))
			}
			restoreBack(old, root, moved)
			return 0, err
		}
		placed = append(placed, e.Name())
	}
	return n, nil
}

// restoreBack undoes the move-aside step. Best effort by necessity: it runs on a path
// that is already failing, and there is nothing better to try if it fails too.
func restoreBack(old, root string, names []string) {
	for _, name := range names {
		_ = os.Rename(filepath.Join(old, name), filepath.Join(root, name))
	}
}
