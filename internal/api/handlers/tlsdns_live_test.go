package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// Against the real cluster and the real internet: every configured hostname gets a
// verdict, and the verdict is logged so it can be compared with what is known about the
// installation (etappe 115). Read-only — DNS lookups and TLS handshakes, no requests.
func TestLiveTLSDNS(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	repoPath := os.Getenv("MATRIXCTRL_CONFIG_REPO")
	if repoPath == "" {
		t.Skip("set MATRIXCTRL_CONFIG_REPO to a config repository")
	}
	kc, err := k8s.New()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := gitpkg.OpenOrInit(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	h := NewTLSDNSHandler(kc, config.NewStore(repoPath, repo), "ess")

	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tls-dns", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Hosts []hostReport `json:"hosts"`
		Node  []string     `json:"node_addresses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Hosts) == 0 {
		t.Fatal("no hostnames in the configuration")
	}
	t.Logf("Node-Adressen: %v", out.Node)
	for _, hst := range out.Hosts {
		origin := "—"
		if hst.Origin != nil {
			origin = hst.Origin.Summary()
		}
		// The hostname itself is the installation's, not a secret, but keep the log
		// short: label, verdict, and the two certificates.
		t.Logf("[%s] %s: %s | außen: %s | Ursprung: %s",
			strings.ToUpper(hst.Level), hst.Label, hst.Summary, hst.Public.Summary(), origin)
		if hst.Summary == "" || hst.Level == "" {
			t.Errorf("%s has no verdict", hst.Label)
		}
	}
}
