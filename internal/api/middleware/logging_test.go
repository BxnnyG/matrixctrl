package middleware

import (
	"net/url"
	"strings"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// The line that started this etappe, taken from the operator's own deploy log.
func TestTheSessionTokenNeverReachesTheLog(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3ODYwMzUzNjB9.YGMIcIcCFdin"
	got := SanitizeURL(mustURL(t, "/api/v1/helm/releases/ess/upgrade/4756b6bd/logs?token="+jwt))

	if strings.Contains(got, jwt) || strings.Contains(got, "eyJ") {
		t.Fatalf("the token survived into the log line: %s", got)
	}
	if !strings.Contains(got, "token=[redacted]") {
		t.Errorf("the parameter should still be visible as redacted, got %s", got)
	}
	if !strings.HasPrefix(got, "/api/v1/helm/releases/ess/upgrade/4756b6bd/logs") {
		t.Errorf("the path must survive intact, got %s", got)
	}
}

func TestEveryCredentialKeyIsRedacted(t *testing.T) {
	for key := range redactedKeys {
		got := SanitizeURL(mustURL(t, "/x?"+key+"=SUPERSECRET"))
		if strings.Contains(got, "SUPERSECRET") {
			t.Errorf("%s was not redacted: %s", key, got)
		}
	}
}

// Blanking the whole query would trade one debugging problem for another: the
// container name is how a reader knows which log was asked for.
func TestOrdinaryParametersSurvive(t *testing.T) {
	got := SanitizeURL(mustURL(t, "/api/v1/pods/x/logs?container=postgres&tail=50"))
	if !strings.Contains(got, "container=postgres") || !strings.Contains(got, "tail=50") {
		t.Errorf("useful parameters were lost: %s", got)
	}
}

func TestMixedQueryKeepsOneAndRedactsTheOther(t *testing.T) {
	got := SanitizeURL(mustURL(t, "/ws?container=synapse&token=abc123&tail=10"))
	if strings.Contains(got, "abc123") {
		t.Fatalf("token leaked: %s", got)
	}
	for _, want := range []string{"container=synapse", "token=[redacted]", "tail=10"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

// url.ParseQuery drops pairs it cannot parse. If the sanitiser used it, a token in a
// malformed query would vanish from the sanitiser's view and reappear in the log.
func TestMalformedQueryIsStillRedacted(t *testing.T) {
	u := &url.URL{Path: "/x", RawQuery: "token=abc%zzdef&%%%&other=1"}
	got := SanitizeURL(u)
	if strings.Contains(got, "abc") {
		t.Fatalf("a token in a malformed query survived: %s", got)
	}
}

func TestCaseInsensitiveKeys(t *testing.T) {
	for _, raw := range []string{"/x?Token=abc", "/x?TOKEN=abc", "/x?ToKeN=abc"} {
		if got := SanitizeURL(mustURL(t, raw)); strings.Contains(got, "abc") {
			t.Errorf("%s was not redacted: %s", raw, got)
		}
	}
}

func TestNoQueryAndNilAreHarmless(t *testing.T) {
	if got := SanitizeURL(mustURL(t, "/api/v1/status")); got != "/api/v1/status" {
		t.Errorf("unexpected: %s", got)
	}
	if got := SanitizeURL(nil); got != "" {
		t.Errorf("nil should render empty, got %q", got)
	}
}

// A bare flag parameter has no value to redact but must not gain a stray "=".
func TestValuelessParameter(t *testing.T) {
	if got := SanitizeURL(mustURL(t, "/x?verbose")); got != "/x?verbose" {
		t.Errorf("unexpected: %s", got)
	}
}
