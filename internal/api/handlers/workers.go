package handlers

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/synmetrics"
)

// WorkersHandler answers "does Synapse need workers, and which" from a minute-by-minute
// record of every Synapse process (etappe 118).
type WorkersHandler struct {
	store   *synmetrics.Store
	latest  func() map[string]synmetrics.Latest
	errs    func() map[string]string
	procs   func(ctx context.Context) ([]k8s.SynapseProcess, error)
	cfg     *config.Store
	verdict time.Duration
}

func NewWorkersHandler(store *synmetrics.Store, latest func() map[string]synmetrics.Latest,
	procs func(context.Context) ([]k8s.SynapseProcess, error), cfg *config.Store) *WorkersHandler {
	return &WorkersHandler{store: store, latest: latest, procs: procs, cfg: cfg, verdict: 7 * 24 * time.Hour}
}

type workerProcess struct {
	k8s.SynapseProcess
	// Running is false for a worker switched on in the configuration that has no pod.
	Running bool `json:"running"`
	Enabled bool `json:"enabled"`
	// Stats over the chosen range; nil before the first recorded minute.
	Stats *synmetrics.Stats `json:"stats,omitempty"`
	// Lifetime is the average since the process started, from the latest reading.
	Lifetime *lifetimeView `json:"lifetime,omitempty"`
}

type lifetimeView struct {
	Cores  float64   `json:"cores"`
	Memory float64   `json:"memory"`
	Since  time.Time `json:"since"`
}

type areaView struct {
	synmetrics.Area
	Cores float64 `json:"cores"`
}

// GET /api/v1/workers?range=24h|7d
func (h *WorkersHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rng := 24 * time.Hour
	if r.URL.Query().Get("range") == "7d" {
		rng = 7 * 24 * time.Hour
	}
	now := time.Now()

	rows, err := h.store.Since(ctx, now.Add(-h.verdict))
	if err != nil {
		Error(w, http.StatusInternalServerError, "Messwerte nicht lesbar: "+err.Error())
		return
	}
	byProcess := map[string][]synmetrics.Row{}
	for _, row := range rows {
		byProcess[row.Process] = append(byProcess[row.Process], row)
	}

	enabled := h.enabledWorkers(ctx)
	latest := map[string]synmetrics.Latest{}
	if h.latest != nil {
		latest = h.latest()
	}
	var live []k8s.SynapseProcess
	if h.procs != nil {
		live, _ = h.procs(ctx)
	}

	var processes []workerProcess
	var mainWeek synmetrics.Stats
	var mainLifetime *synmetrics.Rate
	var workersWeek []synmetrics.Stats
	running := map[string]bool{}
	for _, p := range live {
		running[p.Worker] = true
		wp := workerProcess{SynapseProcess: p, Running: true, Enabled: p.Worker == "main" || enabled[p.Worker]}
		if inRange := within(byProcess[p.Pod], now.Add(-rng)); len(inRange) > 0 {
			st := synmetrics.Aggregate(inRange)
			wp.Stats = &st
		}
		if l, ok := latest[p.Pod]; ok {
			if lt, ok := synmetrics.Lifetime(l.Snapshot); ok {
				wp.Lifetime = &lifetimeView{Cores: lt.Cores, Memory: lt.Memory, Since: time.Unix(int64(l.StartTime), 0)}
				if p.Worker == "main" {
					mainLifetime = &lt
				}
			}
		}
		week := synmetrics.Aggregate(byProcess[p.Pod])
		if p.Worker == "main" {
			mainWeek = week
		} else if week.Samples > 0 {
			workersWeek = append(workersWeek, week)
		}
		processes = append(processes, wp)
	}
	// Switched on, but nothing runs: said, because the configuration and the cluster
	// disagree and the operator believes the first.
	for typ := range enabled {
		if !running[typ] {
			processes = append(processes, workerProcess{SynapseProcess: k8s.SynapseProcess{Worker: typ}, Enabled: true})
		}
	}
	sort.SliceStable(processes, func(i, j int) bool {
		if (processes[i].Worker == "main") != (processes[j].Worker == "main") {
			return processes[i].Worker == "main"
		}
		return processes[i].Worker < processes[j].Worker
	})

	verdict := synmetrics.Judge(mainWeek, mainLifetime, workersWeek, enabled)

	// Where the main process's time goes: from the range when there is one, else from
	// the lifetime counters — and the response says which.
	areas, total, source := mainAreas(processes, mainLifetime)

	JSON(w, http.StatusOK, map[string]any{
		"verdict":       verdict,
		"range":         map[bool]string{true: "7d", false: "24h"}[rng > 24*time.Hour],
		"processes":     processes,
		"areas":         areas,
		"main_cores":    total,
		"areas_source":  source,
		"enabled":       enabled,
		"history":       history(byProcess, now.Add(-rng), rng),
		"interval_secs": int(synmetrics.SamplerInterval.Seconds()),
		"worker_labels": workerLabels(),
		"read_errors":   h.readErrors(),
	})
}

// SetReadErrors wires the sampler's view of processes it cannot read.
func (h *WorkersHandler) SetReadErrors(f func() map[string]string) { h.errs = f }

func (h *WorkersHandler) readErrors() map[string]string {
	if h.errs == nil {
		return map[string]string{}
	}
	return h.errs()
}

func within(rows []synmetrics.Row, cut time.Time) []synmetrics.Row {
	i := sort.Search(len(rows), func(i int) bool { return !rows[i].At.Before(cut) })
	return rows[i:]
}

func mainAreas(processes []workerProcess, lifetime *synmetrics.Rate) ([]areaView, float64, string) {
	var src map[string]float64
	var total float64
	source := ""
	for _, p := range processes {
		if p.Worker != "main" {
			continue
		}
		if p.Stats != nil && p.Stats.Samples > 0 {
			src, total, source = p.Stats.Areas, p.Stats.Avg, "range"
		}
	}
	if source == "" && lifetime != nil {
		src, total, source = lifetime.Areas, lifetime.Cores, "lifetime"
	}
	var out []areaView
	for _, a := range synmetrics.Areas {
		if c := src[a.ID]; c > 0 {
			out = append(out, areaView{Area: a, Cores: c})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cores > out[j].Cores })
	return out, total, source
}

// history buckets each process's minutes for a chart: five minutes over a day, an hour
// over a week. Each point is [unix seconds, average cores, highest minute].
func history(byProcess map[string][]synmetrics.Row, cut time.Time, rng time.Duration) map[string][][3]float64 {
	bucket := 5 * time.Minute
	if rng > 24*time.Hour {
		bucket = time.Hour
	}
	out := map[string][][3]float64{}
	for proc, rows := range byProcess {
		var pts [][3]float64
		var start time.Time
		var sum, max float64
		var n int
		flush := func() {
			if n > 0 {
				pts = append(pts, [3]float64{float64(start.Unix()), sum / float64(n), max})
			}
		}
		for _, row := range within(rows, cut) {
			b := row.At.Truncate(bucket)
			if !b.Equal(start) {
				flush()
				start, sum, max, n = b, 0, 0, 0
			}
			sum += row.Cores
			if row.Cores > max {
				max = row.Cores
			}
			n++
		}
		flush()
		if len(pts) > 0 {
			out[proc] = pts
		}
	}
	return out
}

func (h *WorkersHandler) enabledWorkers(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if h.cfg == nil {
		return out
	}
	contents, err := h.cfg.MergedContent(ctx)
	if err != nil {
		return out
	}
	m, err := config.MergeToMap(contents)
	if err != nil {
		return out
	}
	workers, _ := nestedGet(m, "synapse", "workers").(map[string]interface{})
	for typ, v := range workers {
		if wm, ok := v.(map[string]interface{}); ok && wm["enabled"] == true {
			out[typ] = true
		}
	}
	return out
}

// workerLabels names each worker type by the work it takes over — one list, the areas',
// so the page does not keep a second one that drifts.
func workerLabels() map[string]string {
	out := map[string]string{"main": "Hauptprozess"}
	for _, a := range synmetrics.Areas {
		if a.Worker != "" {
			out[a.Worker] = a.Label
		}
	}
	return out
}
