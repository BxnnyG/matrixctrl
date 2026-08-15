package handlers

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/go-chi/chi/v5"
)

type StatusHandler struct {
	k8s        *k8s.Client
	helm       *helm.Client
	essNS      string
	essRelease string
	frontendFS http.Handler
	// selfNS is the namespace this process runs in, resolved once at construction.
	// Empty outside the cluster, which the diagnostics page treats as "one
	// namespace to report instead of two" rather than as an error (etappe 40).
	selfNS string
}

func NewStatusHandler(k8sClient *k8s.Client, helmClient *helm.Client, essNS, essRelease string, frontendFS http.Handler) *StatusHandler {
	return &StatusHandler{
		k8s:        k8sClient,
		helm:       helmClient,
		essNS:      essNS,
		essRelease: essRelease,
		frontendFS: frontendFS,
		selfNS:     k8s.CurrentNamespace(),
	}
}

type statusResponse struct {
	Release     interface{} `json:"release"`
	Components  interface{} `json:"components"`
	Nodes       interface{} `json:"nodes"`
	EvictedPods int         `json:"evicted_pods"`
}

// Get answers the dashboard poll (every 15 s). The four sources are independent,
// so they run concurrently — serially they added up to roughly the sum of the
// slowest, and the Helm read alone used to dominate the whole response (P1-8).
// Each result is written by exactly one goroutine and read after Wait, so no
// mutex is needed.
func (h *StatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var (
		components interface{}
		nodes      interface{}
		release    interface{}
		evicted    int
		wg         sync.WaitGroup
	)

	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}

	if h.k8s != nil {
		run(func() { components, _ = h.k8s.ComponentHealth(ctx, h.essNS) })
		run(func() { nodes, _ = h.k8s.NodeInfo(ctx) })
		run(func() { evicted = h.k8s.EvictedPodCount(ctx, h.essNS) })
	}
	if h.helm != nil {
		run(func() { release, _ = h.helm.GetRelease(h.essRelease) })
	}

	wg.Wait()

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

// GET /api/v1/status/components/{name}/pods — detailed pods + their events for a
// workload. This is the drill-down behind a dashboard row: it answers *why* a pod
// keeps restarting (last exit reason/code) rather than just how often.
func (h *StatusHandler) ComponentDetail(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "k8s unavailable")
		return
	}
	name := chi.URLParam(r, "name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	pods, events, err := h.k8s.ComponentPods(ctx, h.essNS, name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"name":   name,
		"pods":   pods,
		"events": events,
	})
}

// GET /api/v1/status/events?limit=40&warnings=1 — recent events in the ESS namespace
func (h *StatusHandler) Events(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		JSON(w, http.StatusOK, []interface{}{})
		return
	}
	limit := 40
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	warningsOnly := r.URL.Query().Get("warnings") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	events, err := h.k8s.ListEvents(ctx, h.essNS, limit, warningsOnly)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []k8s.EventInfo{}
	}
	JSON(w, http.StatusOK, events)
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
	logs, err := h.k8s.GetPodLogs(ctx, h.essNS, pod, r.URL.Query().Get("container"), tail)
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
	nodes, _ := h.k8s.NodeInfo(ctx)

	// The namespaces this panel administers, and only those (etappe 40).
	//
	// Both loops below used to reach further. `ListPVCs(ctx, "")` listed persistent
	// volume claims in *every* namespace, and the pod count included `kube-system`.
	// Each was one number on a diagnostics page, and each cost a cluster-wide grant
	// that had to be held permanently — for kube-system, a RoleBinding written into
	// the cluster's most sensitive namespace.
	//
	// The storage panel now shows the storage belonging to the thing this panel
	// manages, which is what a reader of that page already assumes it means.
	scopes := []string{h.essNS}
	if h.selfNS != "" && h.selfNS != h.essNS {
		scopes = append(scopes, h.selfNS)
	}

	var pvcs []k8s.PVCInfo
	podCounts := map[string]int{}
	for _, ns := range scopes {
		if pods, err := h.k8s.ListNamespacePods(ctx, ns); err == nil {
			podCounts[ns] = len(pods)
		}
		// Partial results are kept on purpose: a namespace we cannot read costs its
		// own row, not the whole panel. The same trade ListOwnership makes.
		if found, err := h.k8s.ListPVCs(ctx, ns); err == nil {
			pvcs = append(pvcs, found...)
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"nodes":        conditions,
		"node_metrics": nodes,
		"pvcs":         pvcs,
		"pod_counts":   podCounts,
	})
}

func (h *StatusHandler) ServeFrontend(w http.ResponseWriter, r *http.Request) {
	if h.frontendFS != nil {
		h.frontendFS.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
