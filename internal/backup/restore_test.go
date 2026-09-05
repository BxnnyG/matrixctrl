package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// pack builds an archive in memory from a set of files.
func pack(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := writeFile(tw, name, []byte(body), 0o644, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func manifestJSON(t *testing.T, m Manifest) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadParsesManifestAndContents(t *testing.T) {
	m := Manifest{
		FormatVersion: FormatVersion,
		ESS:           Release{Name: "ess", Chart: "matrix-stack-26.8.0", Revision: 30},
		Tables:        []Table{{Name: "hooks", File: "db/hooks.csv"}},
	}
	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":            manifestJSON(t, m),
		"db/hooks.csv":             "id,name\n1,x\n",
		"config-repo/synapse.yaml": "## comment\n",
		"config-repo/.git/HEAD":    "ref: refs/heads/master\n",
	})))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The version the operator needs to see *before* restoring, so they can tell they
	// are about to put a 26.8.0 configuration onto a different cluster (etappe 69).
	if a.Manifest.ESS.Chart != "matrix-stack-26.8.0" || a.Manifest.ESS.Revision != 30 {
		t.Errorf("ESS release not read back: %+v", a.Manifest.ESS)
	}
	if _, ok := a.tables["hooks"]; !ok {
		t.Error("table CSV missing")
	}
	if _, ok := a.config[".git/HEAD"]; !ok {
		t.Error("git history must survive the round trip")
	}
}

// An unknown layout is refused rather than guessed at: a newer archive may mean
// something different by the same file names.
func TestReadRefusesAnUnknownFormat(t *testing.T) {
	_, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json": manifestJSON(t, Manifest{FormatVersion: FormatVersion + 99}),
	})))
	if err == nil {
		t.Fatal("a future format must be refused")
	}
	if !strings.Contains(err.Error(), "Archivformat") {
		t.Errorf("the error should name the format mismatch: %v", err)
	}
}

func TestReadRefusesSomethingThatIsNotABackup(t *testing.T) {
	if _, err := Read(bytes.NewReader(pack(t, map[string]string{"random.txt": "hi"}))); err == nil {
		t.Error("an archive with no manifest must be refused")
	}
	if _, err := Read(strings.NewReader("not gzip at all")); err == nil {
		t.Error("a non-archive must be refused")
	}
}

// A tar can contain "../", and this one is uploaded by a browser.
func TestReadDoesNotEscapeItsDirectory(t *testing.T) {
	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":                manifestJSON(t, Manifest{FormatVersion: FormatVersion}),
		"config-repo/../../etc/passwd": "root:x:0:0",
	})))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for k := range a.config {
		if strings.Contains(k, "..") {
			t.Errorf("a traversal path survived parsing: %q", k)
		}
	}
}

// The config repository comes back with its history, and the swap is atomic enough that
// a failure does not leave a half-written directory behind.
func TestRestoreConfigRepoReplacesInPlace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config-repo")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "old.yaml"), []byte("gone after restore"), 0o644)

	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":            manifestJSON(t, Manifest{FormatVersion: FormatVersion}),
		"config-repo/synapse.yaml": "## a comment that must survive\n",
		"config-repo/.git/HEAD":    "ref: refs/heads/master\n",
	})))
	if err != nil {
		t.Fatal(err)
	}
	n, err := a.RestoreConfigRepo(root)
	if err != nil {
		t.Fatalf("RestoreConfigRepo: %v", err)
	}
	if n != 2 {
		t.Errorf("restored %d files, want 2", n)
	}
	body, err := os.ReadFile(filepath.Join(root, "synapse.yaml"))
	if err != nil || !strings.Contains(string(body), "must survive") {
		t.Errorf("config file not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "HEAD")); err != nil {
		t.Error("git history must be restored, not just the current values")
	}
	// The previous contents are replaced, not merged — a merge would leave slices from
	// two different configurations side by side.
	if _, err := os.Stat(filepath.Join(root, "old.yaml")); !os.IsNotExist(err) {
		t.Error("the old repository must be replaced, not merged into")
	}
}

// schema_migrations is the live database's bookkeeping about itself.
func TestSchemaMigrationsIsNeverRestored(t *testing.T) {
	if !neverRestored["schema_migrations"] {
		t.Error("restoring schema_migrations would misreport which migrations have run")
	}
}

// E72 moved the archive's contents under matrixctrl/ and homeserver/ while Read() still
// looked at the top level. The root manifest of a full archive carries a matching
// format_version, so nothing was refused: the restore found no tables and no config
// files, and reported success. A silent no-op is the worst failure a backup feature can
// have, which is why this test was written before the fix (etappe 73).
func TestReadUnderstandsAFullArchive(t *testing.T) {
	cfg := Manifest{
		FormatVersion: FormatVersion,
		Tables:        []Table{{Name: "hooks", File: "db/hooks.csv"}},
		ConfigFiles:   1,
	}
	root, err := json.Marshal(FullManifest{
		FormatVersion: FormatVersion,
		Parts:         []string{"matrixctrl/", "homeserver/"},
		Config:        cfg,
		ESS:           Release{Chart: "matrix-stack-26.8.0", Revision: 30},
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":                       string(root),
		"matrixctrl/manifest.json":            manifestJSON(t, cfg),
		"matrixctrl/db/hooks.csv":             "id,name\n1,x\n",
		"matrixctrl/config-repo/synapse.yaml": "## comment\n",
		"homeserver/db/events.csv":            "event_id\n$abc\n",
	})))
	if err != nil {
		t.Fatalf("a full archive must be readable: %v", err)
	}
	if len(a.tables) == 0 {
		t.Error("the database part was not found — a restore would silently do nothing")
	}
	if len(a.config) == 0 {
		t.Error("the config part was not found")
	}
	// Synapse's tables must not be restored into MatrixCtrl's database: they belong to
	// another system and are put back with psql.
	if _, ok := a.tables["events"]; ok {
		t.Error("homeserver tables must not enter MatrixCtrl's restore set")
	}
	// The ESS release must survive, so the preview can still say which version it is.
	if a.Manifest.ESS.Chart != "matrix-stack-26.8.0" {
		t.Errorf("ESS release lost: %+v", a.Manifest.ESS)
	}
}

// TestRestoreOrderPutsParentsFirst covers the ordering on its own, without a database.
//
// The case that matters is the real one: hook_run_log references hooks, and sorts before
// it, so alphabetical order — what the restore used until etappe 74 — is exactly wrong.
func TestRestoreOrderPutsParentsFirst(t *testing.T) {
	before := func(t *testing.T, out []string, first, second string) {
		t.Helper()
		i, j := indexOf(out, first), indexOf(out, second)
		if i < 0 || j < 0 {
			t.Fatalf("%q or %q missing from %v", first, second, out)
		}
		if i > j {
			t.Errorf("%q must come before %q, got %v", first, second, out)
		}
	}

	t.Run("the pair that broke the restore", func(t *testing.T) {
		names := []string{"audit_log", "hook_run_log", "hooks"}
		parents := map[string]map[string]bool{"hook_run_log": {"hooks": true}}
		out := topoSort(names, parents)
		before(t, out, "hooks", "hook_run_log")
		if len(out) != len(names) {
			t.Errorf("lost a table: %v", out)
		}
	})

	t.Run("the pair that worked by luck stays working", func(t *testing.T) {
		names := []string{"config_snapshots", "upgrade_history"}
		parents := map[string]map[string]bool{"upgrade_history": {"config_snapshots": true}}
		before(t, topoSort(names, parents), "config_snapshots", "upgrade_history")
	})

	t.Run("a chain", func(t *testing.T) {
		names := []string{"c", "b", "a"}
		parents := map[string]map[string]bool{"c": {"b": true}, "b": {"a": true}}
		out := topoSort(names, parents)
		if !reflect.DeepEqual(out, []string{"a", "b", "c"}) {
			t.Errorf("got %v", out)
		}
	})

	t.Run("unrelated tables keep name order", func(t *testing.T) {
		names := []string{"a", "b", "c"}
		out := topoSort(names, nil)
		if !reflect.DeepEqual(out, names) {
			t.Errorf("two runs of the same restore must agree: %v", out)
		}
	})

	t.Run("a cycle is restored rather than refused", func(t *testing.T) {
		names := []string{"a", "b"}
		parents := map[string]map[string]bool{"a": {"b": true}, "b": {"a": true}}
		out := topoSort(names, parents)
		if len(out) != 2 {
			t.Fatalf("a cycle must still yield every table, got %v", out)
		}
	})
}

func indexOf(hay []string, needle string) int {
	for i, s := range hay {
		if s == needle {
			return i
		}
	}
	return -1
}

// The directory the restore is given must be the directory it leaves behind.
//
// This is not pedantry about inodes. In the pod that directory is a mount point: the
// config PVC is mounted at /data/config-repo, and the filesystem above it is the
// container's read-only root. A restore that stages beside it and renames it into
// place needs to create an entry in a read-only directory and rename a mount point,
// and the first version of this function did both. It failed for an operator with
// `unlinkat /data/config-repo.restoring: read-only file system`.
//
// Every test of it passed a fresh t.TempDir(), whose parent is writable — so the code
// was exercised only in the one arrangement where the defect is invisible.
func TestRestoreConfigRepoKeepsTheDirectoryItWasGiven(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "config-repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.yaml"), []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}

	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":            manifestJSON(t, Manifest{FormatVersion: FormatVersion}),
		"config-repo/synapse.yaml": "## restored\n",
		"config-repo/.git/HEAD":    "ref: refs/heads/master\n",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RestoreConfigRepo(root); err != nil {
		t.Fatalf("RestoreConfigRepo: %v", err)
	}

	after, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("the config repository was replaced by a different directory; in the pod " +
			"that directory is a mount point and cannot be replaced at all")
	}

	// Nothing may be left in, or ever have needed, the directory above it.
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config-repo" {
			t.Errorf("restore left %q beside the repository, where the pod cannot write", e.Name())
		}
	}

	// The staging directories are the restore's own business and must not survive it.
	for _, name := range []string{restoreStaging, restoreOld} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s left behind inside the repository", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "old.yaml")); !os.IsNotExist(err) {
		t.Error("the previous contents must be replaced, not merged into")
	}
	if body, err := os.ReadFile(filepath.Join(root, "synapse.yaml")); err != nil || !strings.Contains(string(body), "restored") {
		t.Errorf("config file not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "HEAD")); err != nil {
		t.Error("git history must come back with the configuration")
	}
}

// An archive whose paths collide with the staging directories would have those files
// swept aside by the move, and the restore would report a count it did not deliver.
func TestRestoreConfigRepoRefusesReservedPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config-repo")
	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json": manifestJSON(t, Manifest{FormatVersion: FormatVersion}),
		"config-repo/" + restoreStaging + "/sneaky.yaml": "x\n",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RestoreConfigRepo(root); err == nil {
		t.Error("an archive using a reserved staging path must be refused, not partly restored")
	}
}
