package handlers

import (
	"net"
	"net/http"
	"strings"

	"github.com/bxnnyg/matrixctrl/internal/dnscheck"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// recordOrder fixes the order the records are presented in, and recordPurpose says
// what each one costs when it is missing.
//
// The keys are the keys of greenfieldHostnames — the map the deploy actually writes.
// The names are never restated here, only looked up, so a hostname added to the deploy
// cannot quietly fail to appear in the list an operator is told to create. A test
// fails if the two ever diverge.
var recordOrder = []string{
	"serverName",
	"synapse.ingress.host",
	"matrixAuthenticationService.ingress.host",
	"elementWeb.ingress.host",
	"elementAdmin.ingress.host",
	"matrixRTC.ingress.host",
}

var recordPurpose = map[string]string{
	// The one the wizard's footnote left out. Well-known delegation is served at the
	// server name itself, so an operator following that footnote exactly created five
	// records out of six and then wondered why federation did not work.
	"serverName":           "Well-Known-Delegation — ohne diesen Eintrag findet niemand deinen Homeserver",
	"synapse.ingress.host": "Der Homeserver selbst — Clients und Föderation",
	"matrixAuthenticationService.ingress.host": "Anmeldung (MAS) — ohne diesen kommt niemand rein, auch du nicht",
	"elementWeb.ingress.host":                  "Element im Browser",
	"elementAdmin.ingress.host":                "Die Admin-Oberfläche von Element",
	"matrixRTC.ingress.host":                   "Sprach- und Videoanrufe",
}

// greenfieldRecords derives the DNS records a greenfield deploy needs from the same
// map that deploy writes into the config.
func greenfieldRecords(serverName string, overrides map[string]string) []dnscheck.Record {
	hosts := applyHostnameOverrides(greenfieldHostnames(serverName), overrides)
	out := make([]dnscheck.Record, 0, len(recordOrder))
	for _, key := range recordOrder {
		name, ok := hosts[key].(string)
		if !ok || name == "" {
			continue
		}
		out = append(out, dnscheck.Record{Key: key, Type: "A", Name: name, Purpose: recordPurpose[key]})
	}
	return out
}

type dnsTarget struct {
	Addresses []string `json:"addresses"`
	// Where the address came from, in words. An operator comparing a record against a
	// number deserves to know whether MatrixCtrl read it off the node or was told it.
	Source string `json:"source"`
	// False when nothing publicly reachable is known. The UI asks in that case rather
	// than presenting a private address as the value to publish.
	Usable bool   `json:"usable"`
	Note   string `json:"note,omitempty"`
}

type dnsResponse struct {
	ServerName string            `json:"server_name"`
	Target     dnsTarget         `json:"target"`
	Records    []dnscheck.Result `json:"records"`
	AllOK      bool              `json:"all_ok"`
}

// GET /api/v1/setup/dns?server_name=example.com[&target=203.0.113.10]
//
// The step that did not exist. Before this, the entire guidance about the one part of
// the install nobody else can do was an eleven-pixel line naming five prefixes.
func (h *HelmHandler) SetupDNS(w http.ResponseWriter, r *http.Request) {
	serverName := strings.TrimSpace(r.URL.Query().Get("server_name"))
	if serverName == "" {
		Error(w, http.StatusBadRequest, "Ohne Server-Namen lässt sich nicht sagen, welche Einträge gebraucht werden.")
		return
	}
	if strings.ContainsAny(serverName, " /\\:") || !strings.Contains(serverName, ".") {
		Error(w, http.StatusBadRequest, "Das ist kein Domainname: "+serverName)
		return
	}

	// Overrides arrive as repeated ?override=<key>=<value>, keyed like the deploy's own
	// hostname map — so the records checked here are the records that will be created.
	overrides := map[string]string{}
	for _, raw := range r.URL.Query()["override"] {
		if key, value, ok := strings.Cut(raw, "="); ok {
			overrides[key] = value
		}
	}

	target := h.dnsTarget(r)
	records := greenfieldRecords(serverName, overrides)
	results := dnscheck.Check(r.Context(), nil, records, target.Addresses)

	JSON(w, http.StatusOK, dnsResponse{
		ServerName: serverName,
		Target:     target,
		Records:    results,
		AllOK:      dnscheck.AllPointHere(results),
	})
}

// dnsTarget decides what the records should point at, and is honest when it cannot
// know.
//
// A node behind NAT knows only its private address. Printing that into a record table
// would be worse than printing nothing: it is a precise, confident, wrong instruction.
func (h *HelmHandler) dnsTarget(r *http.Request) dnsTarget {
	// An address the operator supplied wins over anything discovered — they can see
	// their router and the cluster cannot.
	if given := strings.TrimSpace(r.URL.Query().Get("target")); given != "" {
		if net.ParseIP(given) == nil {
			return dnsTarget{Source: "operator", Usable: false, Note: "Das ist keine IP-Adresse: " + given}
		}
		return dnsTarget{Addresses: []string{given}, Source: "operator", Usable: true}
	}

	if h.k8s == nil {
		return dnsTarget{Source: "unknown", Usable: false,
			Note: "Kein Cluster-Zugriff — bitte die öffentliche IP dieses Servers selbst angeben."}
	}
	addrs, err := h.k8s.NodeAddresses(r.Context())
	if err != nil {
		return dnsTarget{Source: "unknown", Usable: false,
			Note: "Node-Adressen nicht lesbar: " + err.Error()}
	}

	var external, private []string
	for _, a := range addrs {
		if a.External != "" && !k8s.IsPrivate(a.External) {
			external = append(external, a.External)
			continue
		}
		if a.Internal != "" && !k8s.IsPrivate(a.Internal) {
			// Single-node clusters routinely carry their public address here.
			external = append(external, a.Internal)
			continue
		}
		if a.Internal != "" {
			private = append(private, a.Internal)
		}
	}
	if len(external) > 0 {
		return dnsTarget{Addresses: external, Source: "node", Usable: true}
	}
	note := "Dieser Server kennt nur eine private Adresse"
	if len(private) > 0 {
		note += " (" + strings.Join(private, ", ") + ")"
	}
	note += " — er steht hinter NAT. Bitte die öffentliche IP selbst angeben."
	return dnsTarget{Source: "private-only", Usable: false, Note: note}
}
