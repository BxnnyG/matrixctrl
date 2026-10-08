package synmetrics

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Row is one stored minute of one process.
type Row struct {
	At             time.Time
	Process        string
	Worker         string // "main" or an ESS worker type
	Cores          float64
	Memory         float64
	Areas          map[string]float64
	FedPendingDest float64
	InboundStaging float64
}

// Stats is one process over a window.
type Stats struct {
	Process string             `json:"process"`
	Worker  string             `json:"worker"`
	Samples int                `json:"samples"`
	From    time.Time          `json:"from"`
	To      time.Time          `json:"to"`
	Avg     float64            `json:"avg_cores"`
	P95     float64            `json:"p95_cores"`
	Max     float64            `json:"max_cores"`
	Now     float64            `json:"now_cores"`
	Memory  float64            `json:"memory"`
	Areas   map[string]float64 `json:"areas"` // average cores per area

	AvgFedPendingDest float64 `json:"avg_fed_pending_destinations"`
	MaxInboundStaging float64 `json:"max_inbound_staging"`
}

// Span is how long the window actually covers.
func (s Stats) Span() time.Duration { return s.To.Sub(s.From) }

// Aggregate folds one process's rows (any order) into stats.
func Aggregate(rows []Row) Stats {
	if len(rows) == 0 {
		return Stats{Areas: map[string]float64{}}
	}
	sorted := append([]Row(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	s := Stats{Process: sorted[0].Process, Worker: sorted[0].Worker, Samples: len(sorted),
		From: sorted[0].At, To: sorted[len(sorted)-1].At, Areas: map[string]float64{}}
	cores := make([]float64, len(sorted))
	for i, r := range sorted {
		cores[i] = r.Cores
		s.Avg += r.Cores
		s.Max = math.Max(s.Max, r.Cores)
		s.AvgFedPendingDest += r.FedPendingDest
		s.MaxInboundStaging = math.Max(s.MaxInboundStaging, r.InboundStaging)
		for a, v := range r.Areas {
			s.Areas[a] += v
		}
	}
	n := float64(len(sorted))
	s.Avg /= n
	s.AvgFedPendingDest /= n
	for a := range s.Areas {
		s.Areas[a] /= n
	}
	last := sorted[len(sorted)-1]
	s.Now, s.Memory = last.Cores, last.Memory
	sort.Float64s(cores)
	s.P95 = cores[int(math.Ceil(0.95*n))-1]
	return s
}

// Verdict is the answer to "does this Synapse need workers, and which".
type Verdict struct {
	// measuring | fine | watch | recommend | busy
	Level  string   `json:"level"`
	Title  string   `json:"title"`
	Detail string   `json:"detail"`
	Worker string   `json:"worker,omitempty"`
	Area   string   `json:"area,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

const (
	// minSpan: before an hour of minutes a 95th percentile is three readings.
	minSpan = time.Hour
	// One Synapse process is one Python interpreter and uses at most one core. These
	// are fractions of that ceiling.
	watchAt     = 0.5
	recommendAt = 0.8
	// A worker only helps when one area carries a real share of the load. Below this the
	// honest answer is "busy, but not in one place".
	dominantShare = 0.25
	// Destinations waiting for outgoing federation, on average. A healthy server's
	// queue is near zero between bursts.
	queueDestinations = 50
	// A worker below this, over at least a day, is paying memory for nothing.
	idleCores = 0.02
	idleSpan  = 24 * time.Hour
)

func pct(cores float64) string { return fmt.Sprintf("%.0f %%", cores*100) }

// Judge decides. main is the main process over the window; lifetime is its average
// since it started, used while the window is still too short; enabled are the worker
// types switched on in the configuration.
func Judge(main Stats, lifetime *Rate, workers []Stats, enabled map[string]bool) Verdict {
	v := judgeMain(main, lifetime, enabled)
	for _, w := range workers {
		if w.Span() >= idleSpan && w.Avg < idleCores {
			v.Notes = append(v.Notes, fmt.Sprintf("Der Worker %s nutzt im Schnitt %s eines Kerns und belegt %.0f MB Speicher.",
				w.Worker, pct(w.Avg), w.Memory/1e6))
		}
	}
	if main.MaxInboundStaging > 100 {
		v.Notes = append(v.Notes, fmt.Sprintf("Eingehende Föderation hat sich gestaut: bis zu %.0f Ereignisse warteten auf Verarbeitung.", main.MaxInboundStaging))
	}
	return v
}

func judgeMain(main Stats, lifetime *Rate, enabled map[string]bool) Verdict {
	if main.Samples == 0 || main.Span() < minSpan {
		v := Verdict{Level: "measuring", Title: "Noch zu wenig Messwerte"}
		since := "Gemessen wird jede Minute."
		if main.Samples > 0 {
			since = fmt.Sprintf("Gemessen wird seit %s.", main.From.Local().Format("15:04"))
		}
		if lifetime != nil {
			v.Detail = fmt.Sprintf("Seit dem Start nutzt Synapse im Schnitt %s eines Kerns. Ein Schnitt verbirgt Spitzen — "+
				"belastbar wird es nach einer Stunde Messung, ein ganzes Bild gibt ein Tag. %s", pct(lifetime.Cores), since)
		} else {
			v.Detail = "Belastbar wird es nach einer Stunde Messung, ein ganzes Bild gibt ein Tag. " + since
		}
		return v
	}

	window := describeSpan(main.Span())
	if main.AvgFedPendingDest > queueDestinations && !enabled["federation-sender"] {
		return Verdict{Level: "recommend", Worker: "federation-sender", Area: "federation-out",
			Title: "Ein Worker würde helfen: Föderation senden",
			Detail: fmt.Sprintf("Im Schnitt warten %.0f Server darauf, dass Synapse ihnen etwas schickt (%s). Ein eigener "+
				"Prozess fürs Senden arbeitet die Warteschlange ab, ohne dass Apps darauf warten.", main.AvgFedPendingDest, window)}
	}

	top, topCores := topArea(main, enabled)
	share := 0.0
	if main.Avg > 0 {
		share = topCores / main.Avg
	}
	load := fmt.Sprintf("Synapse nutzt in der Spitze %s eines Kerns (95 %% der Minuten, %s), im Schnitt %s.", pct(main.P95), window, pct(main.Avg))

	switch {
	case main.P95 < watchAt:
		return Verdict{Level: "fine", Title: "Kein Worker nötig",
			Detail: load + " Ein Synapse-Prozess kann einen Kern ausnutzen; Worker lohnen sich, wenn er dauerhaft an diese Grenze " +
				"kommt. Jeder Worker ist ein eigener Prozess mit eigenem Speicher."}
	case main.P95 < recommendAt:
		v := Verdict{Level: "watch", Title: "Beobachten", Detail: load + " Noch reicht ein Prozess."}
		if top.ID != "" {
			v.Detail += fmt.Sprintf(" Den größten erkennbaren Teil trägt „%s“ (%s der Last).", top.Label, pct(share))
		}
		return v
	case top.ID != "" && share >= dominantShare:
		return Verdict{Level: "recommend", Worker: top.Worker, Area: top.ID,
			Title: "Ein Worker würde helfen: " + top.Label,
			Detail: load + fmt.Sprintf(" „%s“ trägt %s davon — ein eigener Prozess (%s) nimmt das dem Hauptprozess ab.",
				top.Label, pct(share), top.Worker)}
	default:
		return Verdict{Level: "busy", Title: "Ausgelastet, aber nicht an einer Stelle",
			Detail: load + " Kein einzelner Bereich trägt ein Viertel der Last, ein Worker für einen davon würde wenig ändern. " +
				"Den Rest teilen sich Replikation, Datenbank und Event-Schleife — dort helfen eher mehr CPU oder eine schnellere Datenbank."}
	}
}

// topArea is the busiest area a worker could take over and that is not already
// switched on.
func topArea(main Stats, enabled map[string]bool) (Area, float64) {
	var best Area
	var bestCores float64
	for _, a := range Areas {
		if a.Worker == "" || enabled[a.Worker] {
			continue
		}
		if c := main.Areas[a.ID]; c > bestCores {
			best, bestCores = a, c
		}
	}
	return best, bestCores
}

func describeSpan(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("letzte %.0f Tage", d.Hours()/24)
	case d >= 2*time.Hour:
		return fmt.Sprintf("letzte %.0f Stunden", d.Hours())
	default:
		return fmt.Sprintf("letzte %.0f Minuten", d.Minutes())
	}
}
