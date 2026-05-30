package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
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
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var components interface{}
	var nodes interface{}
	var evicted int
	if h.k8s != nil {
		components, _ = h.k8s.ComponentHealth(ctx, h.essNS)
		nodes, _ = h.k8s.NodeInfo(ctx)
		evicted = h.k8s.EvictedPodCount(ctx, h.essNS)
	}

	var release interface{}
	if h.helm != nil {
		release, _ = h.helm.GetRelease(h.essRelease)
	}

	JSON(w, http.StatusOK, statusResponse{
		Release:     release,
		Components:  components,
		Nodes:       nodes,
		EvictedPods: evicted,
	})
}

func (h *StatusHandler) Components(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		JSON(w, http.StatusOK, []interface{}{})
		return
	}
	components, err := h.k8s.ComponentHealth(r.Context(), h.essNS)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, components)
}

func (h *StatusHandler) Release(w http.ResponseWriter, r *http.Request) {
	if h.helm == nil {
		Error(w, http.StatusServiceUnavailable, "helm unavailable")
		return
	}
	rel, err := h.helm.GetRelease(h.essRelease)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, rel)
}

func (h *StatusHandler) DeleteEvictedPods(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	deleted, err := h.k8s.DeleteEvictedPods(r.Context(), h.essNS)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}

// GET /api/v1/status/pods/{deployment} — list pods for a deployment in the ESS namespace
func (h *StatusHandler) DeploymentPods(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	deployment := chi.URLParam(r, "deployment")
	pods, err := h.k8s.ListDeploymentPods(r.Context(), h.essNS, deployment)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, pods)
}

// GET /api/v1/status/pods/{pod}/logs?tail=200 — get pod logs
func (h *StatusHandler) PodLogs(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	pod := chi.URLParam(r, "pod")
	tail := int64(200)
	if t := r.URL.Query().Get("tail"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil && v > 0 {
			tail = v
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	logs, err := h.k8s.GetPodLogs(ctx, h.essNS, pod, tail)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"logs": logs})
}

// DELETE /api/v1/status/pods/{pod} — delete (restart) a pod
func (h *StatusHandler) RestartPod(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	pod := chi.URLParam(r, "pod")
	if err := h.k8s.DeletePod(r.Context(), h.essNS, pod); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "pod": pod})
}

// GET /api/v1/status/sysinfo — node conditions, PVCs, pod counts
func (h *StatusHandler) SysInfo(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	conditions, _ := h.k8s.NodeConditions(ctx)
	pvcs, _ := h.k8s.ListPVCs(ctx, "")
	nodes, _ := h.k8s.NodeInfo(ctx)

	// Pod counts per namespace
	podCounts := map[string]int{}
	for _, ns := range []string{h.essNS, "matrixctrl", "kube-system"} {
		pods, err := h.k8s.ListNamespacePods(ctx, ns)
		if err == nil {
			podCounts[ns] = len(pods)
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"nodes":      conditions,
		"node_metrics": nodes,
		"pvcs":       pvcs,
		"pod_counts": podCounts,
	})
}

func (h *StatusHandler) ServeFrontend(w http.ResponseWriter, r *http.Request) {
	if h.frontendFS != nil {
		h.frontendFS.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
