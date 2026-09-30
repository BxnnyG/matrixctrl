package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/dnscheck"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/tlscheck"
)

// TLS & DNS per hostname (etappe 115): what the internet sees, and what the origin
// serves. The three call failures of 2026-09-26…28 were all in the gap between the two.

type TLSDNSHandler struct {
	k8s   *k8s.Client
	store *config.Store
	essNS string
}

func NewTLSDNSHandler(k *k8s.Client, store *config.Store, essNS string) *TLSDNSHandler {
	return &TLSDNSHandler{k8s: k, store: store, essNS: essNS}
}

// hostFields are the configured hostnames, by what they are for. Read from the settings,
// never guessed: a name the operator did not configure is not a name to check.
var hostFields = []struct{ key, path, label, purpose string }{
	{"synapse", "synapse.ingress.host", "Homeserver", "Apps und andere Server verbinden sich hierhin"},
	{"mas", "matrixAuthenticationService.ingress.host", "Anmeldung", "ohne diesen kommt niemand herein"},
	{"element", "elementWeb.ingress.host", "Element Web", "der Client im Browser"},
	{"admin", "elementAdmin.ingress.host", "Element Admin", "die Verwaltungsoberfläche"},
	{"rtc", "matrixRTC.ingress.host", "Anrufe", "Element Call baut hierüber auf"},
	{"wellknown", "serverName", "Server-Name", "die Wegweiser unter /.well-known"},
}

type hostReport struct {
	Key     string `json:"key"`
	Host    string `json:"host"`
	Label   string `json:"label"`
	Purpose string `json:"purpose"`

	DNS dnscheck.Result `json:"dns"`
	// Proxied is true when the answer comes from Cloudflare — read from the `server`
	// header of the response, not from an address list that goes stale.
	Proxied bool   `json:"proxied"`
	Served  string `json:"served,omitempty"` // who answered, verbatim

	Public tlscheck.Cert  `json:"public"`
	Origin *tlscheck.Cert `json:"origin,omitempty"`

	// OriginSecret is the Secret the Ingress expects this host's certificate in, and
	// OriginSecretMissing says it does not exist — the reason the origin falls back to
	// the ingress controller's default. Named, because "self-signed at the origin"
	// alone sends the operator looking in the wrong place.
	OriginSecret        string `json:"origin_secret,omitempty"`
	OriginSecretMissing bool   `json:"origin_secret_missing,omitempty"`

	Summary string `json:"summary"`
	Level   string `json:"level"`
}

// GET /api/v1/tls-dns
func (h *TLSDNSHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	values := map[string]interface{}{}
	if h.store != nil {
		if contents, err := h.store.MergedContent(ctx); err == nil {
			if m, err := config.MergeToMap(contents); err == nil {
				values = m
			}
		}
	}

	var records []dnscheck.Record
	var reports []hostReport
	seen := map[string]bool{}
	for _, f := range hostFields {
		host, _ := nestedGet(values, strings.Split(f.path, ".")...).(string)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		records = append(records, dnscheck.Record{Key: f.key, Type: "A", Name: host, Purpose: f.purpose})
		reports = append(reports, hostReport{Key: f.key, Host: host, Label: f.label, Purpose: f.purpose})
	}
	if len(reports) == 0 {
		JSON(w, http.StatusOK, map[string]interface{}{"hosts": []hostReport{},
			"note": "In den Einstellungen ist noch keine Adresse eingetragen."})
		return
	}

	// Where this server actually is, and where its ingress answers inside the cluster.
	nodeIPs := h.nodeAddresses(ctx)
	originAddr := h.ingressAddress(ctx)
	tlsSecrets := map[string]string{}
	if h.k8s != nil {
		if m, err := h.k8s.IngressTLSSecrets(ctx, h.essNS); err == nil {
			tlsSecrets = m
		}
	}

	dnsResults := dnscheck.Check(ctx, nil, records, nodeIPs)
	byKey := map[string]dnscheck.Result{}
	for _, d := range dnsResults {
		byKey[d.Key] = d
	}

	var wg sync.WaitGroup
	for i := range reports {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rep := &reports[i]
			rep.DNS = byKey[rep.Key]
			rep.Public = tlscheck.Probe(ctx, rep.Host, "")
			rep.Served, rep.Proxied = whoAnswers(ctx, rep.Host)
			if originAddr != "" {
				c := tlscheck.Probe(ctx, rep.Host, originAddr)
				rep.Origin = &c
			}
			if name := tlsSecrets[rep.Host]; name != "" {
				rep.OriginSecret = name
				if data, err := h.k8s.GetSecret(ctx, h.essNS, name); err == nil && len(data) == 0 {
					rep.OriginSecretMissing = true
				}
			}
			rep.Summary, rep.Level = verdict(*rep, len(nodeIPs) > 0)
		}(i)
	}
	wg.Wait()
	sort.Slice(reports, func(i, j int) bool { return reports[i].Key < reports[j].Key })

	out := map[string]interface{}{"hosts": reports, "node_addresses": nodeIPs}
	if originAddr == "" {
		out["origin_note"] = "Ohne Cluster-Zugriff lässt sich nicht prüfen, welches Zertifikat der Server selbst ausliefert."
	}
	JSON(w, http.StatusOK, out)
}

// verdict turns four checks into one sentence. The loudest fault wins, and each one names
// what to do — a row of green and red dots leaves the reader to guess which matters.
func verdict(r hostReport, knowAddresses bool) (string, string) {
	switch {
	case r.DNS.Status == dnscheck.StatusMissing:
		return "Dieser Name existiert im DNS nicht — solange zeigt niemand hierher.", "err"
	case !r.Public.Reachable:
		return "Von außen nicht erreichbar: " + r.Public.Error, "err"
	case r.Public.SelfSigned:
		return "Von außen wird ein selbstsigniertes Zertifikat ausgeliefert — Browser und andere Server lehnen das ab. " +
			"Meist heißt das: der Name zeigt direkt auf den Server, aber es wurde nie ein echtes Zertifikat ausgestellt.", "err"
	case !r.Public.NameMatches, r.Public.Expired:
		return "Zertifikat von außen: " + r.Public.Summary(), "err"
	case r.Origin != nil && r.Proxied && r.Origin.SelfSigned:
		// Not a fault today, and said so: Cloudflare serves a valid certificate and
		// accepts the origin's in "Full" mode. Yellow for a site that works read as an
		// outage (operator, 2026-09-30). It is a note with a cause and a condition.
		why := "Am Server selbst liegt nur das Standardzertifikat des Ingress-Controllers."
		if r.OriginSecretMissing {
			why = fmt.Sprintf("Am Server selbst liegt nur das Standardzertifikat des Ingress-Controllers, weil das eigentliche "+
				"Zertifikat (Secret %s) nie ausgestellt wurde — meist fehlt cert-manager oder kann nicht ausstellen.", r.OriginSecret)
		}
		return "In Ordnung für Besucher: Cloudflare liefert ein gültiges Zertifikat. " + why +
			" Das hält, solange Cloudflare auf SSL/TLS „Full“ steht — mit „Full (strict)“ oder ohne Proxy fällt der Name aus.", "info"
	case r.Origin != nil && !r.Origin.Reachable && r.Proxied:
		return "Von außen gültig (über Cloudflare). Im Cluster ist der Name nicht erreichbar — " +
			"Dienste, die ihn intern aufrufen, laufen in eine Zeitüberschreitung.", "warn"
	case r.DNS.Status == dnscheck.StatusElsewhere && !r.Proxied && knowAddresses:
		return fmt.Sprintf("Zeigt auf %s — das ist nicht dieser Server.", strings.Join(r.DNS.Resolved, ", ")), "warn"
	case r.Public.DaysLeft < 14:
		return r.Public.Summary(), "warn"
	}
	if r.Proxied {
		return fmt.Sprintf("In Ordnung, über Cloudflare. %s", r.Public.Summary()), "ok"
	}
	return "In Ordnung. " + r.Public.Summary(), "ok"
}

// whoAnswers reads the `server` header — Cloudflare names itself there, and that is an
// observation rather than a guess from an address range that changes.
func whoAnswers(ctx context.Context, host string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+host+"/", nil)
	if err != nil {
		return "", false
	}
	resp, err := headClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	server := resp.Header.Get("Server")
	return server, strings.EqualFold(server, "cloudflare")
}

// nodeAddresses are the addresses the names should point at. Private ones are left out:
// behind NAT the public address is genuinely unknowable from in here, and comparing a
// public DNS answer against 192.168.x would report every correct record as wrong (§4.80).
func (h *TLSDNSHandler) nodeAddresses(ctx context.Context) []string {
	if h.k8s == nil {
		return nil
	}
	nodes, err := h.k8s.NodeAddresses(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range nodes {
		for _, a := range []string{n.External, n.Internal} {
			if a != "" && !k8s.IsPrivate(a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// ingressAddress is the ClusterIP of the ingress controller — where the origin answers
// from inside. Empty when it cannot be determined.
func (h *TLSDNSHandler) ingressAddress(ctx context.Context) string {
	if h.k8s == nil {
		return ""
	}
	svcs, err := h.k8s.ServiceIPs(ctx)
	if err != nil {
		return ""
	}
	for _, s := range svcs {
		if (s.Name == "traefik" || s.Name == "ingress-nginx-controller") && s.IP != "" && s.IP != "None" {
			return s.IP
		}
	}
	return ""
}

// headClient asks who answers, without caring whether the certificate is valid — the
// certificate itself is reported by tlscheck, and a HEAD that fails on it would leave
// exactly the broken hosts unidentified.
var headClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}
