package handlers

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/rtc"
)

type RTCHandler struct {
	k8s         *k8s.Client
	configStore *config.Store
	essNS       string
	essRelease  string
	addresses   *rtc.Store
}

func NewRTCHandler(k8sClient *k8s.Client, cfgStore *config.Store, essNS, essRelease string, db *pgxpool.Pool) *RTCHandler {
	return &RTCHandler{
		k8s: k8sClient, configStore: cfgStore, essNS: essNS, essRelease: essRelease,
		addresses: rtc.NewStore(db),
	}
}

// sfuDeployment is the chart's naming, derived from the release rather than
// hardcoded so an adopted release under another name still resolves.
func (h *RTCHandler) sfuDeployment() string { return h.essRelease + "-matrix-rtc-sfu" }

// Status answers "can a call connect?" — with the half that is knowable stated
// as fact and the half that is not stated as unknown (see internal/rtc).
func (h *RTCHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var services []rtc.ServicePort
	if h.k8s != nil {
		exposed, err := h.k8s.NodePorts(ctx, h.essNS)
		if err != nil {
			// A cluster read that fails must not become an all-clear. Report it
			// and let the findings say the port list is unknown.
			Error(w, http.StatusServiceUnavailable, "could not read services: "+err.Error())
			return
		}
		for _, e := range exposed {
			services = append(services, rtc.ServicePort{
				Service:               e.Service,
				Name:                  e.Name,
				Protocol:              e.Protocol,
				NodePort:              e.NodePort,
				ExternalTrafficPolicy: e.ExternalTrafficPolicy,
			})
		}
	}

	host := h.AnnouncedHost(ctx)

	var ips []string
	var resolveErr error
	if host != "" {
		addrs, err := net.DefaultResolver.LookupHost(ctx, host)
		resolveErr = err
		ips = addrs
	}

	// Record what the host resolves to, then judge whether what the SFU announces
	// can still be current. A failure to record is logged and swallowed: losing one
	// data point is a worse reason to fail this page than it is to lose the page.
	var resolved string
	if len(ips) > 0 {
		resolved = ips[0]
	}
	if err := h.addresses.Record(ctx, host, resolved); err != nil {
		log.Printf("rtc: could not record address observation for %q: %v", host, err)
	}

	freshness, freshnessDetail := h.freshness(ctx, host)

	ports := rtc.SFUPorts(services)
	JSON(w, http.StatusOK, map[string]any{
		"announced_host": host,
		"resolved_ips":   ips,
		"ports":          ports,
		"freshness":      freshness,
		"findings":       rtc.AssessWithFreshness(ports, host, ips, resolveErr, freshness, freshnessDetail),
	})
}

// AnnouncedHost reads matrixRTC.ingress.host — what clients are actually told.
// Exported so the background watcher observes the *same* host the page reports on;
// a second reader of the same config key would be one refactor away from drifting.
// An empty result is a valid answer and is reported as such rather than guessed
// at from the server name.
func (h *RTCHandler) AnnouncedHost(ctx context.Context) string {
	if h.configStore == nil {
		return ""
	}
	slice, err := h.configStore.Get(ctx, "matrixRTC")
	if err != nil || slice == nil {
		return ""
	}
	values, err := config.MergeToMap([]string{slice.Content})
	if err != nil {
		return ""
	}
	host, _ := nestedGet(values, "matrixRTC", "ingress", "host").(string)
	return host
}

// freshness compares when the SFU pod started with when the announced host's
// address last changed. Every way of not knowing returns Unknown with the reason —
// never OK, because "we could not tell" reading as "fine" is the defect this whole
// area of the product exists to stop repeating.
func (h *RTCHandler) freshness(ctx context.Context, host string) (rtc.Freshness, string) {
	if host == "" {
		return rtc.FreshnessUnknown, "Für matrixRTC ist kein Hostname konfiguriert."
	}

	obs, err := h.addresses.Newest(ctx, host)
	if err != nil {
		return rtc.FreshnessUnknown, "Die Adress-Historie konnte nicht gelesen werden."
	}
	if h.k8s == nil {
		return rtc.FreshnessUnknown, "Keine Cluster-Verbindung."
	}

	pods, err := h.k8s.ListDeploymentPods(ctx, h.essNS, h.sfuDeployment())
	if err != nil || len(pods) == 0 {
		return rtc.FreshnessUnknown, "Der SFU-Pod ist nicht auffindbar."
	}

	// Newest pod wins: during a replacement both exist briefly, and the old one's
	// start time would produce a stale verdict for a pod that is on its way out.
	var start time.Time
	for _, p := range pods {
		if t := rtc.ParsePodStart(p.StartedAt); t.After(start) {
			start = t
		}
	}

	verdict, why := rtc.AssessFreshness(start, obs)
	switch verdict {
	case rtc.FreshnessStale:
		return verdict, "Die öffentliche Adresse wechselte am " + obs.FirstSeen.Format("02.01. 15:04") +
			", die SFU läuft seit " + start.Format("02.01. 15:04") + "."
	case rtc.FreshnessOK:
		return verdict, "Letzte Adressänderung " + obs.FirstSeen.Format("02.01. 15:04") +
			", SFU-Start " + start.Format("02.01. 15:04") + "."
	default:
		return verdict, capitalise(why) + "."
	}
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// RestartSFU replaces the SFU pod. Deliberately a pod deletion and not a deployment
// rollout: the SFU runs hostNetwork with one replica and maxUnavailable=0, so a
// rolling update deadlocks — the old pod holds the host ports the new one needs to
// become Ready, and the replacement sits Pending forever while the old one keeps
// serving the stale address. Observed on production for 23 minutes, reporting
// nothing wrong (P2-23).
func (h *RTCHandler) RestartSFU(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pods, err := h.k8s.ListDeploymentPods(ctx, h.essNS, h.sfuDeployment())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(pods) == 0 {
		Error(w, http.StatusNotFound, "no SFU pod found")
		return
	}

	deleted := make([]string, 0, len(pods))
	for _, p := range pods {
		if err := h.k8s.DeletePod(ctx, h.essNS, p.Name); err != nil {
			Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		deleted = append(deleted, p.Name)
	}
	JSON(w, http.StatusOK, map[string]any{"restarted": deleted})
}
