package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"sigs.k8s.io/yaml"

	"github.com/bxnnyg/matrixctrl/internal/config"
	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// "Fertig wenn" of etappe 108, against the real release and the real nodes:
// a configuration whose Postgres fits on no node is refused *before* anything is
// committed, with the service and the numbers in the reason.
//
// The configuration store is a temporary repository, so nothing about the installation
// changes — and the refusal happens before Helm is reached, which is the point. The
// counter-probe asks the same verdict for the values that are running and requires it
// to let them through: a check that refuses everything would pass the first half.
func TestLiveApplyRefusesAConfigThatDoesNotFit(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	hc, err := helm.New("ess")
	if err != nil {
		t.Fatal(err)
	}
	kc, err := k8s.New()
	if err != nil {
		t.Fatal(err)
	}
	running, err := hc.GetReleaseValues("ess")
	if err != nil {
		t.Fatal(err)
	}

	// The release's own values are not a safe baseline: on the installation this was
	// written on they ask for more Postgres CPU than the node has, and the pods only run
	// because their resources were patched by hand. Applying them as they are would take
	// Postgres down again — the refusal of exactly that was the first result of this
	// test. The baseline is the release with the Postgres values that were meant to be
	// made durable.
	fits := withPostgres(t, running, "1Gi", "500m", "2Gi")
	store, repo, dir := tempStore(t, fits)
	h := NewHelmHandler(hc, nil, nil, "ess", store, kc, "ess")

	// Counter-probe first: a configuration that fits must not be refused.
	ok := h.verdictFor(context.Background(), "ess", fits)
	if !ok.Rendered {
		t.Fatalf("the values could not be rendered: %s", ok.Note)
	}
	if ok.Blocking {
		t.Fatalf("a configuration that fits is refused — the check refuses everything: %+v", ok.Findings)
	}
	if as := h.verdictFor(context.Background(), "ess", running); as.Blocking {
		t.Logf("Hinweis: die Werte im Release selbst würden abgelehnt: %s", firstBlocked(as))
	}

	// Now the edit that took a server down twice: Postgres asking for more than any node has.
	huge := withPostgres(t, fits, "400Gi", "500m", "400Gi")
	writeValues(t, dir, huge)
	headBefore, _ := repo.HeadSHA()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/helm/releases/ess/apply-config", strings.NewReader(`{"message":"probe"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "ess")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.ApplyConfig(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error   string        `json:"error"`
		Verdict configVerdict `json:"verdict"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, f := range body.Verdict.Findings {
		if f.Level == "blocked" && strings.Contains(f.Workload, "postgres") && f.MemRequestMi > 0 && f.MemAllocatableMi > 0 {
			named = true
			t.Logf("abgelehnt: %s — %s", f.Workload, f.Message)
		}
	}
	if !named {
		t.Errorf("the refusal does not name Postgres with its request and the node's room: %+v", body.Verdict.Findings)
	}
	if headAfter, _ := repo.HeadSHA(); headAfter != headBefore {
		t.Errorf("a refused apply committed anyway: %s → %s", headBefore, headAfter)
	}
	if d, _ := repo.Diff(); d == "" || d[0] == '(' {
		t.Error("the refused edit should still be pending, not lost")
	}
	restarts := map[string]bool{}
	for _, r := range body.Verdict.Restarts {
		restarts[r.Name] = true
	}
	if !restarts["ess-postgres"] {
		t.Errorf("the verdict should predict the Postgres restart: %v", body.Verdict.Restarts)
	}
}

func withPostgres(t *testing.T, values map[string]interface{}, memReq, cpuReq, memLim string) map[string]interface{} {
	t.Helper()
	out := deepCopyMap(t, values)
	pg, _ := out["postgres"].(map[string]interface{})
	if pg == nil {
		pg = map[string]interface{}{}
		out["postgres"] = pg
	}
	pg["resources"] = map[string]interface{}{
		"requests": map[string]interface{}{"memory": memReq, "cpu": cpuReq},
		"limits":   map[string]interface{}{"memory": memLim, "cpu": "1000m"},
	}
	return out
}

func firstBlocked(v configVerdict) string {
	for _, f := range v.Findings {
		if f.Level == "blocked" {
			return f.Message
		}
	}
	return ""
}

func tempStore(t *testing.T, values map[string]interface{}) (*config.Store, *gitpkg.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"slices":[{"name":"all","file":"all.yaml"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config-slices.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := gitpkg.OpenOrInit(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(dir, repo)
	writeValues(t, dir, values)
	if _, err := repo.CommitAll("running", "t", "t@example.org"); err != nil {
		t.Fatal(err)
	}
	return store, repo, dir
}

func writeValues(t *testing.T, dir string, values map[string]interface{}) {
	t.Helper()
	out, err := yaml.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "all.yaml"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func deepCopyMap(t *testing.T, in map[string]interface{}) map[string]interface{} {
	t.Helper()
	b, _ := json.Marshal(in)
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The alias check against a real cluster, both ways: the running release's alias to
// Traefik is silent, and the same alias moved by one address — what a move to another
// cluster does — is reported with Traefik as the suggestion (etappe 109).
func TestLiveStaleHostAliases(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	hc, err := helm.New("ess")
	if err != nil {
		t.Fatal(err)
	}
	kc, err := k8s.New()
	if err != nil {
		t.Fatal(err)
	}
	running, err := hc.GetReleaseValues("ess")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHelmHandler(hc, nil, nil, "ess", nil, kc, "ess")
	ctx := context.Background()

	v := h.verdictFor(ctx, "ess", running)
	if v.AliasesUnchecked {
		t.Fatal("the services could not be listed — this test needs cluster-wide read")
	}
	if len(v.StaleAliases) != 0 {
		t.Fatalf("the running aliases are correct here, nothing should be reported: %+v", v.StaleAliases)
	}

	rtc, _ := running["matrixRTC"].(map[string]interface{})
	aliases, _ := rtc["hostAliases"].([]interface{})
	if len(aliases) == 0 {
		t.Skip("this installation has no RTC hostAlias to move")
	}
	moved := deepCopyMap(t, running)
	a := moved["matrixRTC"].(map[string]interface{})["hostAliases"].([]interface{})[0].(map[string]interface{})
	good := a["ip"].(string)
	a["ip"] = "10.43.254.254"

	v = h.verdictFor(ctx, "ess", moved)
	if len(v.StaleAliases) != 1 || v.StaleAliases[0].Suggest != good {
		t.Fatalf("want one stale alias suggesting %s, got %+v", good, v.StaleAliases)
	}
	t.Logf("gemeldet: %s", v.StaleAliases[0].Message)
}
