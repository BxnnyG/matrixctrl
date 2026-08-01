package handlers

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/rtc"
)

type RTCHandler struct {
	k8s         *k8s.Client
	configStore *config.Store
	essNS       string
}

func NewRTCHandler(k8sClient *k8s.Client, cfgStore *config.Store, essNS string) *RTCHandler {
	return &RTCHandler{k8s: k8sClient, configStore: cfgStore, essNS: essNS}
}

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

	host := h.announcedHost(ctx)

	var ips []string
	var resolveErr error
	if host != "" {
		addrs, err := net.DefaultResolver.LookupHost(ctx, host)
		resolveErr = err
		ips = addrs
	}

	ports := rtc.SFUPorts(services)
	JSON(w, http.StatusOK, map[string]any{
		"announced_host": host,
		"resolved_ips":   ips,
		"ports":          ports,
		"findings":       rtc.Assess(ports, host, ips, resolveErr),
	})
}

// announcedHost reads matrixRTC.ingress.host — what clients are actually told.
// An empty result is a valid answer and is reported as such rather than guessed
// at from the server name.
func (h *RTCHandler) announcedHost(ctx context.Context) string {
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
