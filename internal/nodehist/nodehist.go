// Package nodehist records what the node looked like over time (etappe 59).
//
// Deliberately the same shape as internal/rtc's sampler and retention, which E44 and
// E45 already proved on the same cluster: a timer that writes, a store that reads a
// window, and a retention with a number stated in code rather than buried in a
// migration nobody rereads.
package nodehist

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

const (
	// SamplerInterval matches the RTC sampler. A minute bounds how stale the newest
	// point can be without making the table large: 1440 rows per node per day.
	SamplerInterval = time.Minute

	// Retention is E45's reasoning applied unchanged: ninety days of minute samples is
	// a few megabytes, and this is operational telemetry with no duty to answer for
	// anything — unlike the audit log, whose retention is a compliance question the
	// project deliberately refuses to guess at (P2-19).
	Retention = 90 * 24 * time.Hour

	// pruneInterval — once a day is enough for a table that grows by the minute.
	pruneInterval = 24 * time.Hour
)

// Sample is one reading of one node.
type Sample struct {
	At    time.Time `json:"at"`
	Node  string    `json:"node"`
	CPU   int64     `json:"cpu_used_millis"`
	CPUAl int64     `json:"cpu_alloc_millis"`
	Mem   int64     `json:"mem_used_mi"`
	MemAl int64     `json:"mem_alloc_mi"`
}

type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Record(ctx context.Context, nodes []k8s.NodeInfo) error {
	if s == nil || s.db == nil || len(nodes) == 0 {
		return nil
	}
	for _, n := range nodes {
		// A node the metrics API has not answered for yet reads as zero usage, which
		// would draw a dip that never happened. Capacity is the reliable half, so a
		// row is written only once there is a usage reading to go with it.
		if n.CPUTotalMillis == 0 {
			continue
		}
		if _, err := s.db.Exec(ctx, `
			INSERT INTO node_samples (node, cpu_used_millis, cpu_alloc_millis, mem_used_mi, mem_alloc_mi)
			VALUES ($1, $2, $3, $4, $5)`,
			n.Name, n.CPUUsedMillis, n.CPUTotalMillis, n.MemUsedMi, n.MemTotalMi); err != nil {
			return err
		}
	}
	return nil
}

// Since returns samples newer than a cut-off, oldest first so a chart can draw them
// in order without reversing.
func (s *Store) Since(ctx context.Context, cut time.Time) ([]Sample, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT observed_at, node, cpu_used_millis, cpu_alloc_millis, mem_used_mi, mem_alloc_mi
		FROM node_samples WHERE observed_at > $1 ORDER BY observed_at ASC`, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sample{}
	for rows.Next() {
		var x Sample
		if err := rows.Scan(&x.At, &x.Node, &x.CPU, &x.CPUAl, &x.Mem, &x.MemAl); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CapacityChange is a node whose allocatable differs from what it used to be.
type CapacityChange struct {
	Node string `json:"node"`
	// From and To are the older and the newest allocatable readings.
	FromCPUMillis int64     `json:"from_cpu_millis"`
	ToCPUMillis   int64     `json:"to_cpu_millis"`
	FromMemMi     int64     `json:"from_mem_mi"`
	ToMemMi       int64     `json:"to_mem_mi"`
	At            time.Time `json:"at"`
}

// DetectCapacityChanges finds nodes whose allocatable changed within the samples given.
//
// The reason this package records allocatable at all. On 2026-08-16 the node lost 26 of
// its 32 cores, every reservation on it became unschedulable at the next reboot, and
// nothing in the panel could say the machine had changed — the arithmetic looked
// impossible with no explanation for why it had ever worked (§4.53).
//
// Compares each node's newest sample against its oldest *differing* one, so a change is
// still reported hours later rather than only in the single interval it happened.
func DetectCapacityChanges(samples []Sample) []CapacityChange {
	type pair struct{ first, last Sample }
	byNode := map[string]*pair{}
	for _, s := range samples {
		p, ok := byNode[s.Node]
		if !ok {
			byNode[s.Node] = &pair{first: s, last: s}
			continue
		}
		p.last = s
	}
	var out []CapacityChange
	for node, p := range byNode {
		if p.first.CPUAl == p.last.CPUAl && p.first.MemAl == p.last.MemAl {
			continue
		}
		out = append(out, CapacityChange{
			Node:          node,
			FromCPUMillis: p.first.CPUAl, ToCPUMillis: p.last.CPUAl,
			FromMemMi: p.first.MemAl, ToMemMi: p.last.MemAl,
			At: p.last.At,
		})
	}
	return out
}

// Sampler writes a reading on a timer.
//
// The same argument as the RTC sampler: a history built from page views has gaps
// exactly where nobody was looking, which is most of the time.
type Sampler struct {
	store  *Store
	readFn func(context.Context) ([]k8s.NodeInfo, error)
}

func NewSampler(store *Store, readFn func(context.Context) ([]k8s.NodeInfo, error)) *Sampler {
	return &Sampler{store: store, readFn: readFn}
}

func (s *Sampler) Run(ctx context.Context) {
	if s == nil || s.store == nil || s.readFn == nil {
		return
	}
	t := time.NewTicker(SamplerInterval)
	defer t.Stop()
	prune := time.NewTicker(pruneInterval)
	defer prune.Stop()

	s.once(ctx) // one immediately, so a restart does not leave a minute-wide hole
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.once(ctx)
		case <-prune.C:
			if n, err := s.store.Prune(ctx); err != nil {
				log.Printf("nodehist: prune: %v", err)
			} else if n > 0 {
				log.Printf("nodehist: pruned %d sample(s) older than %s", n, Retention)
			}
		}
	}
}

func (s *Sampler) once(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := s.readFn(c)
	if err != nil {
		return // transient; the next tick tries again and a gap is honest
	}
	if err := s.store.Record(c, nodes); err != nil {
		log.Printf("nodehist: record: %v", err)
	}
}

// Prune drops samples past the retention window.
func (s *Store) Prune(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM node_samples WHERE observed_at < $1`,
		time.Now().Add(-Retention))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
