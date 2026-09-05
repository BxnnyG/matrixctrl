package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The pod's actual filesystem layout, built for real: a read-only directory with a
// writable volume mounted inside it.
//
// The portable test above asserts the property that makes this work (the directory is
// never replaced). This one asserts the thing itself, because the property was inferred
// from an error message and an inference is not a reproduction. It needs CAP_SYS_ADMIN
// and skips without it, so it runs on a developer machine and on a node, and is quietly
// absent on a hosted CI runner.
func TestRestoreConfigRepoOnAReadOnlyParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "data")
	root := filepath.Join(parent, "config-repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Make `parent` read-only, the way readOnlyRootFilesystem does.
	if err := syscall.Mount(parent, parent, "", syscall.MS_BIND, ""); err != nil {
		t.Skipf("cannot bind-mount here (%v) — needs CAP_SYS_ADMIN", err)
	}
	t.Cleanup(func() { _ = syscall.Unmount(parent, syscall.MNT_DETACH) })
	if err := syscall.Mount("", parent, "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
		t.Skipf("cannot remount read-only (%v)", err)
	}

	// ...and mount a writable volume at the config repository, the way the PVC is.
	if err := syscall.Mount("tmpfs", root, "tmpfs", 0, ""); err != nil {
		t.Skipf("cannot mount a tmpfs at the repository (%v)", err)
	}
	t.Cleanup(func() { _ = syscall.Unmount(root, syscall.MNT_DETACH) })

	// Prove the arrangement is the one being claimed before testing anything in it.
	if err := os.WriteFile(filepath.Join(parent, "canary"), nil, 0o644); err == nil {
		t.Fatal("the parent is writable — this test is not reproducing the pod")
	}
	if err := os.WriteFile(filepath.Join(root, "canary"), nil, 0o644); err != nil {
		t.Fatalf("the repository must be writable: %v", err)
	}

	a, err := Read(bytes.NewReader(pack(t, map[string]string{
		"manifest.json":            manifestJSON(t, Manifest{FormatVersion: FormatVersion}),
		"config-repo/synapse.yaml": "## restored onto a read-only parent\n",
		"config-repo/.git/HEAD":    "ref: refs/heads/master\n",
	})))
	if err != nil {
		t.Fatal(err)
	}
	n, err := a.RestoreConfigRepo(root)
	if err != nil {
		t.Fatalf("RestoreConfigRepo on the pod's layout: %v", err)
	}
	if n != 2 {
		t.Errorf("restored %d files, want 2", n)
	}
	body, err := os.ReadFile(filepath.Join(root, "synapse.yaml"))
	if err != nil || !strings.Contains(string(body), "read-only parent") {
		t.Errorf("config file not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "canary")); !os.IsNotExist(err) {
		t.Error("the previous contents must be replaced")
	}
}
