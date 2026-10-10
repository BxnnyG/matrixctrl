package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/dnshealth"
	"github.com/bxnnyg/matrixctrl/internal/masupstream"
)

// DNSHealthHandler serves the cluster's view of name resolution (etappe 119a).
type DNSHealthHandler struct{ check *dnshealth.Checker }

func NewDNSHealthHandler(c *dnshealth.Checker) *DNSHealthHandler { return &DNSHealthHandler{check: c} }

// GET /api/v1/dns-health
func (h *DNSHealthHandler) Get(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, h.check.State())
}

// ConfiguredHosts are the names the settings give to this installation's services —
// the same list TLS & DNS checks, read from the same place.
func ConfiguredHosts(ctx context.Context, store *config.Store) []string {
	if store == nil {
		return nil
	}
	contents, err := store.MergedContent(ctx)
	if err != nil {
		return nil
	}
	values, err := config.MergeToMap(contents)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range hostFields {
		if host, _ := nestedGet(values, strings.Split(f.path, ".")...).(string); host != "" {
			out = append(out, host)
		}
	}
	return out
}

// IssuerHosts are the hosts MAS reaches for external sign-in — the second lookup that
// failed on 2026-10-10, from MAS to the login provider.
func (h *LoginProvidersHandler) IssuerHosts(ctx context.Context) []string {
	ps, err := h.load(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range ps {
		if !p.Enabled {
			continue
		}
		issuer := p.Issuer
		switch p.Kind {
		case masupstream.Google:
			issuer = "https://accounts.google.com"
		case masupstream.GitHub:
			issuer = "https://github.com"
		}
		if u, err := url.Parse(issuer); err == nil && u.Hostname() != "" {
			out = append(out, u.Hostname())
		}
	}
	return out
}
