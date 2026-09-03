package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildArchive runs the real assembly over a temp config repo and a stub dumper.
func buildArchive(t *testing.T, man Manifest, repo string, withConfig bool) map[string]string {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	dump := func(tb Table) ([]byte, error) { return []byte("id\n1\n"), nil }
	if err := assemble(tw, man, dump, repo, withConfig); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	tw.Close()
	gz.Close()

	zr, err := gzip.NewReader(&out)
	if err != nil {
		t.Fatalf("archive is not valid gzip: %v", err)
	}
	files := map[string]string{}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("archive is not a valid tar: %v", err)
		}
		b, _ := io.ReadAll(tr)
		files[h.Name] = string(b)
	}
	return files
}

func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// A config repo is a git repo; the history is most of its value.
	os.MkdirAll(filepath.Join(dir, ".git", "refs"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/master\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "synapse.yaml"), []byte("## a comment that must survive\nsynapse: {}\n"), 0o644)
	return dir
}

func man(tables ...Table) Manifest {
	return Manifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		AppVersion:    "0.1.63",
		Tables:        tables,
		NotIncluded:   []string{"Die Datenbank von Synapse — der eigentliche Homeserver."},
		SchemaNote:    "Enthält nur Daten, kein Schema.",
	}
}

// The whole point of the etappe: the archive states what it does not hold. An operator
// who believes they have a backup of their homeserver and learns otherwise mid-restore
// is the failure this feature exists to avoid (§4.66).
func TestManifestNamesWhatIsMissing(t *testing.T) {
	files := buildArchive(t, man(), testRepo(t), true)
	raw, ok := files["manifest.json"]
	if !ok {
		t.Fatal("every archive must carry a manifest")
	}
	var got Manifest
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if len(got.NotIncluded) == 0 {
		t.Error("the manifest must say what the archive does not contain")
	}
	if !strings.Contains(strings.Join(got.NotIncluded, " "), "Synapse") {
		t.Errorf("the homeserver must be named as missing: %v", got.NotIncluded)
	}
	if got.SchemaNote == "" {
		t.Error("the manifest must explain why no schema is carried")
	}
	if got.FormatVersion != FormatVersion {
		t.Errorf("format version = %d", got.FormatVersion)
	}
}

// The git history is the reason the config repo is worth backing up at all: without it
// a restore is a snapshot rather than an auditable record.
func TestConfigRepoIncludesGitHistory(t *testing.T) {
	files := buildArchive(t, man(), testRepo(t), true)
	if _, ok := files["config-repo/.git/HEAD"]; !ok {
		t.Errorf("the .git directory must be in the archive, got %v", keys(files))
	}
	body, ok := files["config-repo/synapse.yaml"]
	if !ok {
		t.Fatal("config files must be in the archive")
	}
	// Comments are the field documentation in the ESS chart (§ config editor).
	if !strings.Contains(body, "## a comment that must survive") {
		t.Error("config files must be copied verbatim, comments and all")
	}
}

func TestTablesLandUnderTheirManifestPath(t *testing.T) {
	files := buildArchive(t, man(
		Table{Name: "hooks", Rows: 2, File: "db/hooks.csv"},
		Table{Name: "node_samples", Rows: 4943, Regenerable: true, File: "db/node_samples.csv"},
	), testRepo(t), true)

	for _, want := range []string{"db/hooks.csv", "db/node_samples.csv"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s, got %v", want, keys(files))
		}
	}
}

// Telemetry is included rather than dropped — a backup that quietly omits things is the
// defect — but labelled, so a restore can decide.
func TestTelemetryIsLabelledNotOmitted(t *testing.T) {
	files := buildArchive(t, man(
		Table{Name: "node_samples", Rows: 4943, Regenerable: true, File: "db/node_samples.csv"},
	), testRepo(t), true)
	if _, ok := files["db/node_samples.csv"]; !ok {
		t.Error("regenerable tables must still be in the archive")
	}
	var got Manifest
	json.Unmarshal([]byte(files["manifest.json"]), &got)
	if len(got.Tables) != 1 || !got.Tables[0].Regenerable {
		t.Errorf("node_samples must be marked regenerable: %+v", got.Tables)
	}
}

// A missing config repo must not cost the database backup — half an archive beats none,
// as long as the manifest is honest about it.
func TestArchiveStillFormsWithoutTheConfigRepo(t *testing.T) {
	files := buildArchive(t, man(Table{Name: "hooks", File: "db/hooks.csv"}), "/nonexistent", false)
	if _, ok := files["manifest.json"]; !ok {
		t.Error("manifest must still be written")
	}
	if _, ok := files["db/hooks.csv"]; !ok {
		t.Error("the database must still be captured")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
