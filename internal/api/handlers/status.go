package handlers

import (
	"net/http"

	"github.com/bxnny/matrixctrl/internal/helm"
	"github.com/bxnny/matrixctrl/internal/k8s"
)

type StatusHandler struct {
	k8s        *k8s.Client
	helm       *helm.Client
	essNS      string
	essRelease string
	frontendFS http.Handler
}

func NewStatusHandler(k8sClient *k8s.Client, helmClient *helm.Client, essNS, essRelease string, frontendFS http.Handler) *StatusHandler {
	return &StatusHandler{
		k8s:        k8sClient,
		helm:       helmClient,
		essNS:      essNS,
		essRelease: essRelease,
		frontendFS: frontendFS,
	}
}

type statusResponse struct {
	Release     interface{} `json:"release"`
	Components  interface{} `json:"components"`
	Nodes       interface{} `json:"nodes"`
	EvictedPods int         `json:"evicted_pods"`
}

func (h *StatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	components, _ := h.k8s.ComponentHealth(r.Context(), h.essNS)
	release, _ := h.helm.GetRelease(h.essRelease)
	nodes, _ := h.k8s.NodeInfo(r.Context())
	evicted := h.k8s.EvictedPodCount(r.Context(), h.essNS)

	JSON(w, http.StatusOK, statusResponse{
		Release:     release,
		Components:  components,
		Nodes:       nodes,
		EvictedPods: evicted,
	})
}

func (h *StatusHandler) Components(w http.ResponseWriter, r *http.Request) {
	components, err := h.k8s.ComponentHealth(r.Context(), h.essNS)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, components)
}

func (h *StatusHandler) Release(w http.ResponseWriter, r *http.Request) {
	rel, err := h.helm.GetRelease(h.essRelease)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, rel)
}

func (h *StatusHandler) DeleteEvictedPods(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.k8s.DeleteEvictedPods(r.Context(), h.essNS)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}

func (h *StatusHandler) ServeFrontend(w http.ResponseWriter, r *http.Request) {
	if h.frontendFS != nil {
		h.frontendFS.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
