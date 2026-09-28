package git

import (
	"os"
	"path/filepath"
	"testing"
)

// "Verwerfen" puts the settings back to the last commit, and the pending-changes bar
// disappears because there is no diff left (etappe 108).
func TestDiscardChangesRestoresTheLastCommit(t *testing.T) {
	dir := t.TempDir()
	repo, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "synapse.yaml")
	if err := os.WriteFile(file, []byte("synapse:\n  ## docs survive\n  replicas: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitAll("initial", "t", "t@example.org"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, []byte("synapse:\n  replicas: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d, _ := repo.Diff(); d == "" {
		t.Fatal("the edit should show up as a diff before discarding")
	}

	if err := repo.DiscardChanges(); err != nil {
		t.Fatalf("discard: %v", err)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "synapse:\n  ## docs survive\n  replicas: 1\n" {
		t.Errorf("the file is not back to the last commit, comments included: %q", got)
	}
	if d, _ := repo.Diff(); d != "" && d[0] != '(' {
		t.Errorf("nothing should be pending after discarding: %q", d)
	}
}

// After a failed apply the settings go back to what they were before it — as a new
// commit, so the history still shows what was tried and that it was taken back
// (etappe 108). A hard reset would pass the content check and fail the history one.
func TestRestoreCommitRevertsAsANewCommit(t *testing.T) {
	dir := t.TempDir()
	repo, err := OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "postgres.yaml")
	extra := filepath.Join(dir, "added.yaml")
	good := "postgres:\n  ## docs survive\n  memory: 1Gi\n"
	if err := os.WriteFile(file, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitAll("good", "t", "t@example.org"); err != nil {
		t.Fatal(err)
	}
	before, err := repo.HeadSHA()
	if err != nil {
		t.Fatal(err)
	}

	// The apply that fails: a changed file and a new one.
	_ = os.WriteFile(file, []byte("postgres:\n  memory: 40Gi\n"), 0o644)
	_ = os.WriteFile(extra, []byte("x: 1\n"), 0o644)
	if _, err := repo.CommitAll("bad", "t", "t@example.org"); err != nil {
		t.Fatal(err)
	}

	sha, err := repo.RestoreCommit(before, "Rücksprung", "t", "t@example.org")
	if err != nil || sha == "" {
		t.Fatalf("restore: sha=%q err=%v", sha, err)
	}
	if got, _ := os.ReadFile(file); string(got) != good {
		t.Errorf("content is not back to the good commit: %q", got)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Errorf("a file added by the failed apply should be gone: %v", err)
	}
	if d, _ := repo.Diff(); d != "" && d[0] != '(' {
		t.Errorf("nothing should be pending after the restore: %q", d)
	}
	log, _ := repo.Log(10)
	if len(log) != 3 {
		t.Fatalf("history should keep good, bad and the restore — got %d commits", len(log))
	}

	// Already there: nothing to commit, no error.
	now, _ := repo.HeadSHA()
	if sha, err := repo.RestoreCommit(now, "noop", "t", "t@example.org"); err != nil || sha != "" {
		t.Errorf("restoring the current commit should be a no-op: sha=%q err=%v", sha, err)
	}
}
