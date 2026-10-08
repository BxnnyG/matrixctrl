package synmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// SamplerInterval matches nodehist and RTC: a minute bounds staleness, and a
	// percentile over a week of minutes is 10 080 readings.
	SamplerInterval = time.Minute
	// Retention: a month of minutes per process. Enough to see a weekly rhythm four
	// times over; telemetry, not a record anyone must answer for (as E45 argued).
	Retention     = 30 * 24 * time.Hour
	pruneInterval = 24 * time.Hour
	scrapeTimeout = 5 * time.Second
	metricsLimit  = 8 << 20
)

// listKey is where Errors reports a failure to list the processes at all.
const listKey = "Synapse-Pods auflisten"

// Target is one process to read.
type Target struct {
	Process string
	Worker  string
	URL     string
}

type recorder interface {
	Record(ctx context.Context, process, worker string, r Rate) error
}

// Sampler reads every Synapse process each minute and records the rate since the last
// reading. It keeps the latest reading of each in memory: the lifetime average it
// carries is what the page can say before the history exists.
type Sampler struct {
	store   recorder
	prune   func(context.Context) (int64, error)
	targets func(context.Context) ([]Target, error)
	client  *http.Client
	now     func() time.Time

	mu   sync.Mutex
	last map[string]Snapshot
	seen map[string]string // process → worker, for Latest
	// errs is why a process could not be read, until it can be again. Without it a
	// sampler that never reaches Synapse — a NetworkPolicy, a moved port — looks
	// exactly like one that has not run long enough.
	errs map[string]string
}

func NewSampler(store *Store, targets func(context.Context) ([]Target, error)) *Sampler {
	s := newSampler(store, targets)
	if store != nil {
		s.prune = store.Prune
	}
	return s
}

func newSampler(store recorder, targets func(context.Context) ([]Target, error)) *Sampler {
	return &Sampler{store: store, targets: targets, client: &http.Client{Timeout: scrapeTimeout},
		now: time.Now, last: map[string]Snapshot{}, seen: map[string]string{}, errs: map[string]string{}}
}

func (s *Sampler) Run(ctx context.Context) {
	if s == nil || s.targets == nil {
		return
	}
	t := time.NewTicker(SamplerInterval)
	defer t.Stop()
	prune := time.NewTicker(pruneInterval)
	defer prune.Stop()
	s.Once(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Once(ctx)
		case <-prune.C:
			if s.prune == nil {
				continue
			}
			if n, err := s.prune(ctx); err != nil {
				log.Printf("synmetrics: prune: %v", err)
			} else if n > 0 {
				log.Printf("synmetrics: pruned %d sample(s) older than %s", n, Retention)
			}
		}
	}
}

// Once reads every target and records what it can. A process that cannot be read this
// minute leaves a gap — an honest one — and keeps its previous reading, so the next
// successful one still pairs with it.
func (s *Sampler) Once(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	targets, err := s.targets(c)
	if err != nil {
		// Reported like a process that cannot be read: a sampler that cannot even list
		// Synapse is otherwise silent, and the page would only ever say "too little data".
		s.mu.Lock()
		if s.errs[listKey] != err.Error() {
			log.Printf("synmetrics: cannot list Synapse processes: %v", err)
		}
		s.errs[listKey] = err.Error()
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	delete(s.errs, listKey)
	s.mu.Unlock()
	present := map[string]bool{}
	for _, t := range targets {
		present[t.Process] = true
		snap, err := s.read(c, t.URL)
		if err != nil {
			s.mu.Lock()
			if s.errs[t.Process] != err.Error() {
				log.Printf("synmetrics: cannot read %s: %v", t.Process, err)
			}
			s.errs[t.Process] = err.Error()
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		delete(s.errs, t.Process)
		prev, had := s.last[t.Process]
		s.last[t.Process] = snap
		s.seen[t.Process] = t.Worker
		s.mu.Unlock()
		if !had || s.store == nil {
			continue
		}
		if r, ok := Between(prev, snap); ok {
			if err := s.store.Record(c, t.Process, t.Worker, r); err != nil {
				log.Printf("synmetrics: record %s: %v", t.Process, err)
			}
		}
	}
	// A pod that is gone stays gone: its successor is a new process with a new name.
	s.mu.Lock()
	for p := range s.last {
		if !present[p] {
			delete(s.last, p)
			delete(s.seen, p)
		}
	}
	for p := range s.errs {
		if p != listKey && !present[p] {
			delete(s.errs, p)
		}
	}
	s.mu.Unlock()
}

// Errors is why each process that currently cannot be read cannot be.
func (s *Sampler) Errors() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.errs))
	for p, e := range s.errs {
		out[p] = e
	}
	return out
}

func (s *Sampler) read(ctx context.Context, url string) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	return Parse(io.LimitReader(resp.Body, metricsLimit), s.now())
}

// Latest is the most recent reading of each process, with its worker type.
func (s *Sampler) Latest() map[string]Latest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Latest, len(s.last))
	for p, snap := range s.last {
		out[p] = Latest{Worker: s.seen[p], Snapshot: snap}
	}
	return out
}

// Latest pairs a reading with the worker type it came from.
type Latest struct {
	Worker string
	Snapshot
}

// Store keeps the minutes in Postgres.
type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Record(ctx context.Context, process, worker string, r Rate) error {
	if s == nil || s.db == nil {
		return nil
	}
	areas, err := json.Marshal(r.Areas)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO synapse_samples (process, worker, cores, mem_bytes, areas, fed_pending_destinations, inbound_staging)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		process, worker, r.Cores, int64(r.Memory), areas, r.FedPendingDestinations, r.InboundStaging)
	return err
}

// Since returns every row newer than cut, oldest first.
func (s *Store) Since(ctx context.Context, cut time.Time) ([]Row, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT observed_at, process, worker, cores, mem_bytes, areas, fed_pending_destinations, inbound_staging
		FROM synapse_samples WHERE observed_at >= $1 ORDER BY observed_at`, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var mem int64
		var areas []byte
		if err := rows.Scan(&r.At, &r.Process, &r.Worker, &r.Cores, &mem, &areas, &r.FedPendingDest, &r.InboundStaging); err != nil {
			return nil, err
		}
		r.Memory = float64(mem)
		r.Areas = map[string]float64{}
		_ = json.Unmarshal(areas, &r.Areas)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Prune(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM synapse_samples WHERE observed_at < $1`, time.Now().Add(-Retention))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
