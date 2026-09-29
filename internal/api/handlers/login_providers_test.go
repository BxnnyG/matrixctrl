package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bxnnyg/matrixctrl/internal/config"
	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
)

// fakeCluster keeps Secrets in memory and records restarts.
type fakeCluster struct {
	secrets  map[string]map[string][]byte
	mounted  []string
	restarts int
}

func (f *fakeCluster) GetSecret(_ context.Context, ns, name string) (map[string][]byte, error) {
	if d, ok := f.secrets[ns+"/"+name]; ok {
		return d, nil
	}
	return map[string][]byte{}, nil
}
func (f *fakeCluster) PutSecret(_ context.Context, ns, name string, data map[string][]byte) error {
	f.secrets[ns+"/"+name] = data
	return nil
}
func (f *fakeCluster) SecretVolumes(context.Context, string, string) ([]string, error) {
	return f.mounted, nil
}
func (f *fakeCluster) RolloutRestart(context.Context, string, string, string) error {
	f.restarts++
	return nil
}

const masSection = `matrixAuthenticationService:
  ## Additional configuration to provide to Matrix Authentication Service.
  additional: {}
  ingress:
    ## The host of the authentication service
    host: auth.example.org
`

func loginFixture(t *testing.T) (*LoginProvidersHandler, *fakeCluster, string) {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "config-slices.json"),
		[]byte(`{"slices":[{"name":"matrixAuthenticationService","file":"matrixAuthenticationService.yaml"}]}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "matrixAuthenticationService.yaml"), []byte(masSection), 0o644))
	repo, err := gitpkg.OpenOrInit(dir)
	must(err)
	_, err = repo.CommitAll("init", "t", "t@example.org")
	must(err)
	// No network in tests: discovery "fails", so entered issuers are kept as typed.
	// Tests about discovery replace this themselves.
	old := discover
	discover = func(context.Context, string) (string, int, error) { return "", 0, errors.New("offline") }
	t.Cleanup(func() { discover = old })
	fc := &fakeCluster{secrets: map[string]map[string][]byte{}}
	return NewLoginProvidersHandler(fc, config.NewStore(dir, repo), "ess", "ess"), fc, dir
}

func call(t *testing.T, h http.HandlerFunc, method, path, body string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

const secret = "GOCSPX-this-is-the-client-secret"

// The whole assistant, start to end — and the secret appears in exactly one place:
// the Kubernetes Secret MAS reads. Not in a response, not in the settings repository.
func TestTheClientSecretStaysInTheSecret(t *testing.T) {
	h, fc, dir := loginFixture(t)
	var responses []string

	rec := call(t, h.Create, "POST", "/", `{"kind":"google"}`, nil)
	responses = append(responses, rec.Body.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created publicProvider
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Callback, "https://auth.example.org/upstream/callback/") || created.Complete {
		t.Fatalf("a draft with its callback: %+v", created)
	}

	rec = call(t, h.Update, "PUT", "/"+created.ID, `{"client_id":"abc.apps.googleusercontent.com","client_secret":"`+secret+`"}`, map[string]string{"id": created.ID})
	responses = append(responses, rec.Body.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"next":"apply"`) {
		t.Errorf("first activation should add the mount as a pending change: %s", rec.Body)
	}
	responses = append(responses, call(t, h.List, "GET", "/", "", nil).Body.String())

	for i, r := range responses {
		if strings.Contains(r, secret) {
			t.Errorf("response %d carries the client secret: %s", i, r)
		}
	}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), secret) {
			t.Errorf("the settings repository holds the secret in %s", strings.TrimPrefix(p, dir))
		}
		return nil
	})
	stored := fc.secrets["ess/"+upstreamSecret]
	if !strings.Contains(string(stored[upstreamConfigKey]), secret) {
		t.Error("the Secret MAS reads should hold it")
	}

	// The mount went into the settings, pointing at the Secret.
	mas, _ := os.ReadFile(filepath.Join(dir, "matrixAuthenticationService.yaml"))
	if !strings.Contains(string(mas), "configSecret: "+upstreamSecret) || !strings.Contains(string(mas), "## The host of the authentication service") {
		t.Errorf("mount missing, or comments lost:\n%s", mas)
	}
}

// Once MAS mounts the Secret, a change to it restarts MAS — the pod template does not
// change, so nothing else would.
func TestAChangeToAMountedSecretRestartsMAS(t *testing.T) {
	h, fc, _ := loginFixture(t)
	var created publicProvider
	_ = json.Unmarshal(call(t, h.Create, "POST", "/", `{"kind":"github"}`, nil).Body.Bytes(), &created)
	call(t, h.Update, "PUT", "/", `{"client_id":"a","client_secret":"b"}`, map[string]string{"id": created.ID})
	fc.mounted = []string{"ess-generated", upstreamSecret}

	rec := call(t, h.Update, "PUT", "/", `{"enabled":false}`, map[string]string{"id": created.ID})
	if !strings.Contains(rec.Body.String(), `"next":"restarting"`) || fc.restarts != 1 {
		t.Errorf("want a restart: %s (restarts %d)", rec.Body, fc.restarts)
	}
	// Switched off, and still there — MAS keeps removed entries until a prune.
	if !strings.Contains(string(fc.secrets["ess/"+upstreamSecret][upstreamConfigKey]), "enabled: false") {
		t.Error("disabled provider must be rendered as enabled: false")
	}
	if rec := call(t, h.Delete, "DELETE", "/", "", map[string]string{"id": created.ID}); rec.Code != http.StatusConflict {
		t.Errorf("a configured provider must not be deletable: %d", rec.Code)
	}
}

// Without the authentication host there is no callback URL — and none is invented.
func TestNoHostNoProvider(t *testing.T) {
	h, _, dir := loginFixture(t)
	_ = os.WriteFile(filepath.Join(dir, "matrixAuthenticationService.yaml"), []byte("matrixAuthenticationService:\n  additional: {}\n"), 0o644)
	if rec := call(t, h.Create, "POST", "/", `{"kind":"google"}`, nil); rec.Code != http.StatusConflict {
		t.Errorf("want 409 without a host, got %d", rec.Code)
	}
}

// Editing a draft touches nothing MAS reads: no mount as a pending change, no restart.
func TestEditingADraftActivatesNothing(t *testing.T) {
	h, fc, dir := loginFixture(t)
	fc.mounted = []string{upstreamSecret}
	var created publicProvider
	_ = json.Unmarshal(call(t, h.Create, "POST", "/", `{"kind":"oidc","issuer":"https://id.example.org/"}`, nil).Body.Bytes(), &created)
	rec := call(t, h.Update, "PUT", "/", `{"issuer":"https://id.example.org"}`, map[string]string{"id": created.ID})
	if !strings.Contains(rec.Body.String(), `"next":"unchanged"`) || fc.restarts != 0 {
		t.Errorf("a draft edit must not activate: %s (restarts %d)", rec.Body, fc.restarts)
	}
	if mas, _ := os.ReadFile(filepath.Join(dir, "matrixAuthenticationService.yaml")); strings.Contains(string(mas), "configSecret") {
		t.Error("a draft edit must not add the mount")
	}
}

// The first real Zitadel: entered with a trailing slash, named without one — MAS refused
// with "issuer URLs don't match". The provider's own spelling is adopted when that is the
// only difference, and a genuinely different issuer is left alone.
func TestTheProvidersSpellingOfTheIssuerWins(t *testing.T) {
	h, fc, _ := loginFixture(t)
	discover = func(context.Context, string) (string, int, error) { return "https://auth.example.org", 200, nil }
	var created publicProvider
	_ = json.Unmarshal(call(t, h.Create, "POST", "/", `{"kind":"oidc","issuer":"https://auth.example.org/"}`, nil).Body.Bytes(), &created)
	if created.Issuer != "https://auth.example.org" {
		t.Errorf("create kept %q", created.Issuer)
	}
	call(t, h.Update, "PUT", "/", `{"issuer":"https://auth.example.org/","client_id":"a","client_secret":"b"}`, map[string]string{"id": created.ID})
	if !strings.Contains(string(fc.secrets["ess/"+upstreamSecret][upstreamConfigKey]), "issuer: https://auth.example.org\n") {
		t.Errorf("rendered issuer: %s", fc.secrets["ess/"+upstreamSecret][upstreamConfigKey])
	}
	// Counter-probe: a different issuer is not "corrected" into the discovered one.
	call(t, h.Update, "PUT", "/", `{"issuer":"https://other.example.org"}`, map[string]string{"id": created.ID})
	if !strings.Contains(string(fc.secrets["ess/"+upstreamSecret][upstreamConfigKey]), "issuer: https://other.example.org\n") {
		t.Error("a different issuer must be kept as typed")
	}
}
