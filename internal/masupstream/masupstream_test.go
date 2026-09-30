package masupstream

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func providers(t *testing.T, doc string) []map[string]interface{} {
	t.Helper()
	var v struct {
		Upstream struct {
			Providers []map[string]interface{} `yaml:"providers"`
		} `yaml:"upstream_oauth2"`
	}
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, doc)
	}
	return v.Upstream.Providers
}

var all = []Provider{
	{ID: "01HFS6S2SVAR7Y7QYMZJ53ZAGZ", Kind: Google, ClientID: "g", ClientSecret: "gs", Enabled: true},
	{ID: "01HFS67GJ145HCM9ZASYS9DC3J", Kind: GitHub, ClientID: "h", ClientSecret: "hs", Enabled: true},
	{ID: "01HFVBY12TMNTYTBV8W921M5FA", Kind: OIDC, Name: "Firma", Issuer: "https://id.example.org/", ClientID: "o", ClientSecret: "os", Enabled: true},
}

// The takeover path MAS warns about: a provider name matching an existing account.
// Every kind must leave the Matrix username to the user, and none may set on_conflict.
func TestNoProviderTakesTheUsernameFromOutside(t *testing.T) {
	out, err := Render(all)
	if err != nil {
		t.Fatal(err)
	}
	ps := providers(t, out)
	if len(ps) != 3 {
		t.Fatalf("want 3 providers, got %d", len(ps))
	}
	for _, p := range ps {
		lp := p["claims_imports"].(map[string]interface{})["localpart"].(map[string]interface{})
		if lp["action"] != "ignore" {
			t.Errorf("%v: localpart action %v, want ignore", p["human_name"], lp["action"])
		}
		if _, set := lp["on_conflict"]; set {
			t.Errorf("%v: on_conflict must stay at MAS's default (fail)", p["human_name"])
		}
		email := p["claims_imports"].(map[string]interface{})["email"].(map[string]interface{})
		if email["action"] != "suggest" {
			t.Errorf("%v: email should be offered, not forced: %v", p["human_name"], email["action"])
		}
	}
}

// A draft has a callback URL but no credentials yet. Rendered, it would stop MAS from
// starting — the login of everyone, for a provider nobody can use yet.
func TestDraftsAreNotRendered(t *testing.T) {
	draft := Provider{ID: "01HFS6S2SVAR7Y7QYMZJ53ZAGZ", Kind: Google, Enabled: true}
	out, err := Render([]Provider{draft, all[1]})
	if err != nil {
		t.Fatal(err)
	}
	if ps := providers(t, out); len(ps) != 1 || ps[0]["brand_name"] != "github" {
		t.Errorf("only the complete provider may be rendered: %v", ps)
	}
	if out, _ := Render(nil); len(providers(t, out)) != 0 || !strings.Contains(out, "providers: []") {
		t.Errorf("no providers must still be valid config: %q", out)
	}
}

// Switched off, not removed: MAS keeps removed entries until a prune.
func TestDisablingKeepsTheEntry(t *testing.T) {
	off := all[0]
	off.Enabled = false
	out, _ := Render([]Provider{off})
	ps := providers(t, out)
	if len(ps) != 1 || ps[0]["enabled"] != false {
		t.Errorf("a disabled provider must be rendered with enabled: false: %v", ps)
	}
}

// A secret is data, whatever it contains.
func TestASecretCannotBreakOutOfItsString(t *testing.T) {
	p := all[0]
	p.ClientSecret = "x\"\n  evil: true\n# '"
	out, err := Render([]Provider{p})
	if err != nil {
		t.Fatal(err)
	}
	ps := providers(t, out)
	if ps[0]["client_secret"] != p.ClientSecret {
		t.Errorf("secret mangled: %q", ps[0]["client_secret"])
	}
	if _, injected := ps[0]["evil"]; injected {
		t.Error("the secret injected a key")
	}
}

func TestGitHubUsesOAuthNotOIDC(t *testing.T) {
	out, _ := Render([]Provider{all[1]})
	p := providers(t, out)[0]
	if p["discovery_mode"] != "disabled" || p["fetch_userinfo"] != true || p["userinfo_endpoint"] != "https://api.github.com/user" {
		t.Errorf("GitHub needs the manual OAuth setup: %v", p)
	}
}

func TestULID(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	a, err := NewULID(now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewULID(now)
	if !regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`).MatchString(a) {
		t.Errorf("not a ULID: %s", a)
	}
	if a == b {
		t.Error("two ULIDs in the same millisecond must still differ")
	}
	// The first ten characters encode the time: equal for the same millisecond.
	if a[:10] != b[:10] {
		t.Errorf("time prefix differs: %s %s", a, b)
	}
	later, _ := NewULID(now.Add(time.Hour))
	if later[:10] <= a[:10] {
		t.Errorf("ULIDs must sort by time: %s !> %s", later, a)
	}
}

// The address that had to be handed over by hand before it was shown in the product.
func TestLinkURL(t *testing.T) {
	if got := LinkURL("auth.example.org", "01HFS6S2SVAR7Y7QYMZJ53ZAGZ"); got != "https://auth.example.org/upstream/authorize/01HFS6S2SVAR7Y7QYMZJ53ZAGZ" {
		t.Error(got)
	}
	if got := AccountURL("auth.example.org/"); got != "https://auth.example.org/account/" {
		t.Error(got)
	}
}

func TestCallback(t *testing.T) {
	if got := Callback("auth.example.org", "01HFS6S2SVAR7Y7QYMZJ53ZAGZ"); got != "https://auth.example.org/upstream/callback/01HFS6S2SVAR7Y7QYMZJ53ZAGZ" {
		t.Error(got)
	}
}

// Zitadel and friends: the profile is in userinfo, not in the ID token.
func TestGenericOIDCFetchesUserinfo(t *testing.T) {
	out, _ := Render([]Provider{all[2]})
	p := providers(t, out)[0]
	if p["fetch_userinfo"] != true || p["issuer"] != "https://id.example.org/" {
		t.Errorf("generic OIDC should read userinfo: %v", p)
	}
}
