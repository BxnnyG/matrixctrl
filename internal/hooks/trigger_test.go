package hooks

import "testing"

// A rollback recreates objects from the old revision's manifests, so it drops manual
// patches exactly as an upgrade does. Every "re-apply my patch" hook must therefore run
// after both, without the operator keeping two copies of it (etappe 61).
func TestRollbackAlsoRunsPatchRestoringHooks(t *testing.T) {
	got := triggeredBy(TriggerPostRollback)
	if len(got) != 2 {
		t.Fatalf("rollback must run two trigger classes, got %v", got)
	}
	has := func(w TriggerType) bool {
		for _, g := range got {
			if g == w {
				return true
			}
		}
		return false
	}
	if !has(TriggerPostRollback) || !has(TriggerPostUpgrade) {
		t.Errorf("rollback must run post-rollback and post-upgrade, got %v", got)
	}
}

// The reverse must not hold: a hook written for rolling back would be a surprise after
// an ordinary upgrade.
func TestUpgradeDoesNotRunRollbackHooks(t *testing.T) {
	got := triggeredBy(TriggerPostUpgrade)
	if len(got) != 1 || got[0] != TriggerPostUpgrade {
		t.Errorf("upgrade must run only post-upgrade hooks, got %v", got)
	}
}

func TestManualIsUnaffected(t *testing.T) {
	got := triggeredBy(TriggerManual)
	if len(got) != 1 || got[0] != TriggerManual {
		t.Errorf("got %v", got)
	}
}
