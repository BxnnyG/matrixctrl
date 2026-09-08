package handlers

import "testing"

// A configuration as it stands after a greenfield deploy on old.example.com, with one
// hostname the operator chose themselves.
func configOn(server string, custom map[string]string) map[string]interface{} {
	m := map[string]interface{}{"serverName": server}
	set := func(path, v string) {
		// Two levels is all these keys have: section.ingress.host.
		parts := []string{}
		cur := ""
		for _, c := range path {
			if c == '.' {
				parts = append(parts, cur)
				cur = ""
				continue
			}
			cur += string(c)
		}
		parts = append(parts, cur)
		node := m
		for _, p := range parts[:len(parts)-1] {
			if node[p] == nil {
				node[p] = map[string]interface{}{}
			}
			node = node[p].(map[string]interface{})
		}
		node[parts[len(parts)-1]] = v
	}
	for key, v := range greenfieldHostnames(server) {
		if key == "serverName" {
			continue
		}
		set(key, v.(string))
	}
	for key, v := range custom {
		set(key, v)
	}
	return m
}

func find(p *renamePreview, key string) *renameChange {
	for i := range p.Changes {
		if p.Changes[i].Key == key {
			return &p.Changes[i]
		}
	}
	return nil
}

// The one that is always forgotten by hand: well-known delegation is served at the
// server name itself, so a rename that misses it leaves federation pointing at a
// domain nobody answers on (§4.81).
func TestRenameIncludesTheServerNameItself(t *testing.T) {
	plan := renamePlanFrom(configOn("old.example.com", nil), "new.example.com")

	c := find(plan, "serverName")
	if c == nil {
		t.Fatal("serverName is not in the plan; a rename that misses it breaks federation silently")
	}
	if c.From != "old.example.com" || c.To != "new.example.com" {
		t.Errorf("serverName: %q → %q", c.From, c.To)
	}
	if len(plan.Changes) != len(recordOrder) {
		t.Errorf("%d changes, want %d — every derived hostname moves with the domain", len(plan.Changes), len(recordOrder))
	}
}

// A hostname somebody chose by hand is shown, and marked as not derived. Rewriting it
// silently would undo a decision the rename did not make.
func TestRenameDoesNotClaimAHandPickedHostAsItsOwn(t *testing.T) {
	cfg := configOn("old.example.com", map[string]string{
		"synapse.ingress.host": "chat.somewhere-else.net",
	})
	plan := renamePlanFrom(cfg, "new.example.com")

	custom := find(plan, "synapse.ingress.host")
	if custom == nil {
		t.Fatal("the hand-picked host is missing from the plan; it must be shown, just not assumed")
	}
	if custom.Derived {
		t.Error("a host that is not what the old server name would have produced must not be reported as derived")
	}
	if derived := find(plan, "matrixAuthenticationService.ingress.host"); derived == nil || !derived.Derived {
		t.Error("the untouched hosts are derived and must say so")
	}
}

// After a partial rename, "already correct" is more useful than a missing row.
func TestRenameReportsWhatIsAlreadyRight(t *testing.T) {
	cfg := configOn("old.example.com", map[string]string{
		"elementWeb.ingress.host": "element.new.example.com",
	})
	plan := renamePlanFrom(cfg, "new.example.com")

	if find(plan, "elementWeb.ingress.host") != nil {
		t.Error("a value that already matches the target is not a change")
	}
	var seen bool
	for _, k := range plan.Unchanged {
		if k == "elementWeb.ingress.host" {
			seen = true
		}
	}
	if !seen {
		t.Error("it should be listed as unchanged rather than silently omitted")
	}
}

// Renaming to the name it already has is a no-op, not a six-line diff.
func TestRenameToTheSameNameChangesNothing(t *testing.T) {
	plan := renamePlanFrom(configOn("example.com", nil), "example.com")
	if len(plan.Changes) != 0 {
		t.Errorf("%d changes for a rename to the same name", len(plan.Changes))
	}
	if len(plan.Unchanged) != len(recordOrder) {
		t.Errorf("%d unchanged, want %d", len(plan.Unchanged), len(recordOrder))
	}
}
