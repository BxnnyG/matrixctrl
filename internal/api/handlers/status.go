package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/nodehist"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// nodes serves the recorded node history. Nil disables the endpoint rather than
	// making it answer with an empty series, which would read as "nothing happened".
	nodes *nodehist.Store
	// backupDB and backupRepo are what an archive is made of (etappe 68).
	backupDB   *pgxpool.Pool
	backupRepo string
	appVersion string
}

// SetBackup wires what a backup needs: the database and the config repository path
// (etappe 68). Nil disables the endpoint rather than producing an empty archive, which
// would be a backup that looks taken and holds nothing.
func (h *StatusHandler) SetBackup(db *pgxpool.Pool, configRepo, appVersion string) {
	h.backupDB, h.backupRepo, h.appVersion = db, configRepo, appVersion
}

// GET /api/v1/backup — the config repository and MatrixCtrl's own database.
//
// Streamed straight to the response: the archive is small today, but buffering it is a
// decision that only turns wrong on a larger install, and then it is a crash rather
// than a slow download.
func (h *StatusHandler) Backup(w http.ResponseWriter, r *http.Request) {
	if h.backupDB == nil {
		Error(w, http.StatusServiceUnavailable, "Für diese Installation ist kein Backup verfügbar.")
		return
	}
	name := "matrixctrl-backup-" + time.Now().UTC().Format("2006-01-02-1504") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)

	// The managed release, so a restore can reproduce the same version rather than the
	// newest one (etappe 69). Best effort: a backup without it is still worth having,
	// and an empty field says "unknown" rather than inventing a version.
	var ess backup.Release
	if h.helm != nil {
		if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
			ess = backup.Release{
				Name: h.essRelease, Namespace: h.essNS,
				Chart: rel.Version, Revision: rel.Revision,
			}
		}
	}

	if err := backup.Create(r.Context(), h.backupDB, h.backupRepo, h.appVersion, ess, w); err != nil {
		// The status line is already sent, so this cannot become a clean 500. Logged
		// so a truncated archive has an explanation somewhere, rather than being a
		// file that unpacks halfway and is discovered during a restore.
		log.Printf("backup: failed partway through: %v", err)
	}
}

// GET /api/v1/status/backup/full — everything reachable, in one file (etappe 72).
//
// The page used to offer two downloads and three warning blocks explaining what each
// half could not do. Every sentence was true and the arrangement still showed the order
// the features were built in rather than the operator's task. "The backup" should be one
// thing you can hold.
//
// Synapse's database is best-effort: without it the archive is still worth having, and
// the manifest says which parts are inside.
func (h *StatusHandler) BackupFull(w http.ResponseWriter, r *http.Request) {
	if h.backupDB == nil {
		Error(w, http.StatusServiceUnavailable, "Für diese Installation ist kein Backup verfügbar.")
		return
	}

	var hs *pgx.Conn
	if h.k8s != nil {
		if dsn, err := h.synapseDSN(r.Context()); err == nil {
			if conn, cerr := pgx.Connect(r.Context(), dsn); cerr == nil {
				hs = conn
				defer conn.Close(context.Background())
			} else {
				// Logged, never echoed: the message carries the DSN and the DSN carries
				// the password.
				log.Printf("full backup: synapse unreachable, continuing without it: %v", cerr)
			}
		}
	}

	name := "matrixctrl-full-" + time.Now().UTC().Format("2006-01-02-1504") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)

	if err := backup.CreateFull(r.Context(), h.backupDB, hs, h.backupRepo, h.appVersion, h.essRelease_(r), w); err != nil {
		log.Printf("full backup: failed partway through: %v", err)
	}
}

// essRelease_ reads the managed release for a manifest, best effort.
func (h *StatusHandler) essRelease_(r *http.Request) backup.Release {
	var ess backup.Release
	if h.helm != nil {
		if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
			ess = backup.Release{Name: h.essRelease, Namespace: h.essNS, Chart: rel.Version, Revision: rel.Revision}
		}
	}
	return ess
}

// GET /api/v1/status/backup/homeserver — Synapse's own database (etappe 70).
//
// Separate from the configuration archive because it answers a different question. That
// one rebuilds the deployment; this one is what makes a rebuilt server *the same* server
// — the accounts, the rooms, the 19 000 events.
//
// Credentials come from the cluster secret and are never logged. The export is one
// REPEATABLE READ snapshot, which is the difference between a backup and a pile of
// reads taken at different moments.
func (h *StatusHandler) BackupHomeserver(w http.ResponseWriter, r *http.Request) {
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "Ohne Cluster-Zugriff ist kein Export möglich.")
		return
	}
	dsn, err := h.synapseDSN(r.Context())
	if err != nil {
		Error(w, http.StatusBadGateway, "Die Zugangsdaten für Synapses Datenbank konnten nicht gelesen werden: "+err.Error())
		return
	}

	conn, err := pgx.Connect(r.Context(), dsn)
	if err != nil {
		// Deliberately not echoing the error: a connection failure can carry the DSN,
		// and the DSN carries the password.
		log.Printf("homeserver export: connect failed: %v", err)
		Error(w, http.StatusBadGateway, "Synapses Datenbank ist nicht erreichbar.")
		return
	}
	defer conn.Close(context.Background())

	name := "synapse-db-" + time.Now().UTC().Format("2006-01-02-1504") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)

	if err := backup.ExportHomeserver(r.Context(), conn, "synapse", w); err != nil {
		log.Printf("homeserver export: failed partway through: %v", err)
	}
}

// synapseDSN builds the connection string from the cluster secret.
//
// The password is read per request and never stored on the handler: a credential held
// in a struct for the life of the process is one that outlives the reason it was needed.
func (h *StatusHandler) synapseDSN(ctx context.Context) (string, error) {
	pw, err := h.k8s.SecretValue(ctx, h.essNS, "ess-generated", "POSTGRES_SYNAPSE_PASSWORD")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("postgres://synapse_user:%s@ess-postgres.%s.svc.cluster.local:5432/synapse?sslmode=disable",
		url.QueryEscape(pw), h.essNS), nil
}

// POST /api/v1/status/restore/preview — read an archive without changing anything.
//
// Separate from the restore itself on purpose: an operator should see the ESS release
// and the contents of an archive *before* it overwrites what is there, not discover
// afterwards that they put a 26.8.0 configuration onto a different cluster (etappe 69).
func (h *StatusHandler) RestorePreview(w http.ResponseWriter, r *http.Request) {
	a, err := h.readArchive(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, a.Manifest)
}

// POST /api/v1/status/restore — write an archive back.
//
// Destructive, and the reason restore arrived an etappe after backup. The database goes
// back in one transaction; the config repository is written beside the live one and
// swapped, so a failure part-way leaves what was there.
func (h *StatusHandler) Restore(w http.ResponseWriter, r *http.Request) {
	if h.backupDB == nil {
		Error(w, http.StatusServiceUnavailable, "Für diese Installation ist keine Wiederherstellung verfügbar.")
		return
	}
	a, err := h.readArchive(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	files, err := a.RestoreConfigRepo(h.backupRepo)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Das Konfigurations-Repository konnte nicht wiederhergestellt werden: "+err.Error())
		return
	}
	tables, err := a.RestoreDatabase(r.Context(), h.backupDB)
	if err != nil {
		// The config repository is already back and the database is not. Said plainly
		// rather than reported as a clean failure, because the two halves are now from
		// different points in time and the operator has to know which.
		Error(w, http.StatusInternalServerError,
			"Die Konfiguration wurde wiederhergestellt, die Datenbank nicht: "+err.Error()+
				" — die Datenbank ist unverändert, das Archiv kann erneut eingespielt werden.")
		return
	}

	log.Printf("restore: %d config file(s), tables: %s", files, strings.Join(tables, ", "))
	JSON(w, http.StatusOK, map[string]any{
		"config_files":   files,
		"tables":         tables,
		"ess":            a.Manifest.ESS,
		"restart_needed": true,
	})
}

// readArchive is the shared upload path. Bounded: an unbounded read of an uploaded file
// is how a panel is turned off by a large POST.
func (h *StatusHandler) readArchive(r *http.Request) (*backup.Archive, error) {
	return backup.Read(io.LimitReader(r.Body, 512<<20))
}

// SetNodeHistory wires the recorded node samples (etappe 59).
func (h *StatusHandler) SetNodeHistory(s *nodehist.Store) { h.nodes = s }

// GET /api/v1/status/nodes/history — recorded node usage and capacity.
//
// Replaces a browser-side ref that died on reload and, worse, pre-filled itself with
// the current value so a fresh page drew a flat line that read as an hour of stability
// (P2-3). What is returned here was measured; a gap in it is a gap.
func (h *StatusHandler) NodeHistory(w http.ResponseWriter, r *http.Request) {
	if h.nodes == nil {
		Error(w, http.StatusServiceUnavailable, "Für diese Installation wird kein Node-Verlauf aufgezeichnet.")
		return
	}
	hours := 6
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 24*90 {
		hours = v
	}
	samples, err := h.nodes.Since(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		Error(w, http.StatusInternalServerError, "Der Node-Verlauf konnte nicht gelesen werden.")
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"samples": samples,
		// The reason the capacity columns exist at all (§4.53).
		"capacity_changes": nodehist.DetectCapacityChanges(samples),
		"interval_seconds": int(nodehist.SamplerInterval.Seconds()),
	})
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
