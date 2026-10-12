package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/bridges"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/tasks"
)

// bridgeCluster is what the page reads from Kubernetes.
type bridgeCluster interface {
	LabelledPods(ctx context.Context, namespace, selector string) ([]k8s.LabelledPod, error)
	GetConfigMap(ctx context.Context, namespace, name string) (map[string]string, error)
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
}

// BridgesHandler shows Hookshot — part of ESS — and every other bridge Synapse knows
// through an appservice registration (etappe 119).
type BridgesHandler struct {
	k8s   bridgeCluster
	store *config.Store
	essNS string
}

func NewBridgesHandler(k bridgeCluster, store *config.Store, essNS string) *BridgesHandler {
	return &BridgesHandler{k8s: k, store: store, essNS: essNS}
}

type hookshotView struct {
	Enabled       bool              `json:"enabled"`
	Pods          []k8s.LabelledPod `json:"pods"`
	Version       string            `json:"version,omitempty"`
	Bot           string            `json:"bot"`
	WebhookPrefix string            `json:"webhook_prefix,omitempty"`
	Webhooks      bool              `json:"webhooks"`
	Feeds         bool              `json:"feeds"`
}

type appserviceView struct {
	// Source is where the registration lives, for the operator to find it.
	Source       string                `json:"source"`
	Registration *bridges.Registration `json:"registration,omitempty"`
	Error        string                `json:"error,omitempty"`
	// Reach is "" when the bridge's address accepts connections from inside the cluster.
	Reach string `json:"reach"`
}

// GET /api/v1/bridges
func (h *BridgesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	values := map[string]interface{}{}
	if h.store != nil {
		if contents, err := h.store.MergedContent(ctx); err == nil {
			if m, err := config.MergeToMap(contents); err == nil {
				values = m
			}
		}
	}
	JSON(w, http.StatusOK, map[string]any{
		"hookshot":    h.hookshot(ctx, values),
		"appservices": h.appservices(ctx, values),
	})
}

func str(values map[string]interface{}, path ...string) string {
	s, _ := nestedGet(values, path...).(string)
	return s
}

func (h *BridgesHandler) hookshot(ctx context.Context, values map[string]interface{}) hookshotView {
	v := hookshotView{Enabled: nestedGet(values, "hookshot", "enabled") == true}
	local := str(values, "hookshot", "user", "localpart")
	if local == "" {
		local = "hookshot"
	}
	v.Bot = "@" + local + ":" + str(values, "serverName")
	// The same derivation as the chart's config-override: its own host when it has
	// one, otherwise under the homeserver's.
	if host := str(values, "hookshot", "ingress", "host"); host != "" {
		v.WebhookPrefix = "https://" + host + "/webhook"
	} else if host := str(values, "synapse", "ingress", "host"); host != "" {
		v.WebhookPrefix = "https://" + host + "/_matrix/hookshot/webhook"
	}
	// The two functions as the settings card reads them — one reading, two places.
	var fields []tasks.Field
	for _, c := range tasks.Cards() {
		if c.ID == "bridges" {
			fields = c.Fields
		}
	}
	read := tasks.Read(values, fields)
	v.Webhooks = read["hookshot.webhooks"].Value == true
	v.Feeds = read["hookshot.feeds"].Value == true
	if h.k8s != nil {
		if pods, err := h.k8s.LabelledPods(ctx, h.essNS, "app.kubernetes.io/name=hookshot"); err == nil {
			v.Pods = pods
			for _, p := range pods {
				if i := strings.LastIndex(p.Image, ":"); i > 0 && v.Version == "" {
					v.Version = p.Image[i+1:]
				}
			}
		}
	}
	return v
}

func (h *BridgesHandler) appservices(ctx context.Context, values map[string]interface{}) []appserviceView {
	list, _ := nestedGet(values, "synapse", "appservices").([]interface{})
	out := []appserviceView{}
	for _, item := range list {
		m, _ := item.(map[string]interface{})
		if m == nil {
			continue
		}
		var raw []byte
		var view appserviceView
		var err error
		switch {
		case m["secret"] != nil:
			name, key := asString(m["secret"]), asString(m["secretKey"])
			view.Source = "Secret " + name + " / " + key
			var data map[string][]byte
			if h.k8s != nil {
				if data, err = h.k8s.GetSecret(ctx, h.essNS, name); err == nil {
					raw = data[key]
				}
			}
		case m["configMap"] != nil:
			name, key := asString(m["configMap"]), asString(m["configMapKey"])
			view.Source = "ConfigMap " + name + " / " + key
			var data map[string]string
			if h.k8s != nil {
				if data, err = h.k8s.GetConfigMap(ctx, h.essNS, name); err == nil {
					raw = []byte(data[key])
				}
			}
		default:
			continue
		}
		switch {
		case err != nil:
			view.Error = "nicht lesbar: " + err.Error()
		case len(raw) == 0:
			view.Error = "der Eintrag ist leer oder fehlt"
		default:
			reg, perr := bridges.Parse(raw)
			if perr != nil {
				view.Error = perr.Error()
			} else {
				view.Registration = &reg
				view.Reach = bridges.Reach(ctx, reg.URL, nil)
			}
		}
		out = append(out, view)
	}
	return out
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}
