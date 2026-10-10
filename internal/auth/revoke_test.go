package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Every sign-in used to leave its MAS session open for good — the operator's session
// list filled with "MatrixCtrl" entries (etappe 119d). After the sign-in, the token it
// came with has to be revoked, whether the sign-in was allowed or refused.
func TestASignInEndsItsMASSession(t *testing.T) {
	var mu sync.Mutex
	revoked := map[string]string{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
				"userinfo_endpoint": srv.URL + "/userinfo", "revocation_endpoint": srv.URL + "/revoke"})
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"login-access-token","token_type":"Bearer"}`))
		case "/userinfo":
			_, _ = w.Write([]byte(`{"sub":"01HUSER00000000000000000AA"}`))
		case "/revoke":
			_ = r.ParseForm()
			mu.Lock()
			revoked[r.PostForm.Get("token")] = r.PostForm.Get("token_type_hint")
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for name, allowed := range map[string][]string{"allowed": {"01HUSER00000000000000000AA"}, "refused": {"01HSOMEONEELSE000000000000"}} {
		mu.Lock()
		revoked = map[string]string{}
		mu.Unlock()
		o, err := NewOIDCService(OIDCConfig{Issuer: srv.URL, ClientID: "c", ClientSecret: "s", RedirectURI: "https://panel/cb", AllowedUsers: allowed}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = o.LoginWithCode(context.Background(), "code")
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			hint, ok := revoked["login-access-token"]
			mu.Unlock()
			if ok {
				if hint != "access_token" {
					t.Errorf("%s: hint %q", name, hint)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: the sign-in's token was never revoked", name)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// A grant replaced by a new one, or forgotten at sign-out, ends its MAS session; keeping
// the same one does not.
func TestLettingGoOfAGrantRevokesIt(t *testing.T) {
	m := NewMatrixTokens(nil)
	var got []string
	m.SetRevoker(func(rt string) { got = append(got, rt) })

	m.Put("@op:example.com", "a1", "r1", 300)
	m.Put("@op:example.com", "a2", "r1", 300) // same grant, new access token: nothing to end
	if len(got) != 0 {
		t.Fatalf("revoked %v while keeping the grant", got)
	}
	m.Put("@op:example.com", "a3", "r2", 300) // reconnected: the old grant goes
	m.Forget("@op:example.com")               // signed out: the new one goes too
	if len(got) != 2 || got[0] != "r1" || got[1] != "r2" {
		t.Fatalf("revoked %v, want [r1 r2]", got)
	}
	m.Forget("@op:example.com") // nothing left: nothing to revoke
	if len(got) != 2 {
		t.Fatalf("revoked %v after a second sign-out", got)
	}
}
