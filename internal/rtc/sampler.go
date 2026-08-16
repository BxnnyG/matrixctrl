package rtc

import (
	"context"
	"log"
	"time"
)

// Sampler records what the SFU reports, on a timer.
//
// Same argument as Watcher, for a different quantity: "a history built from page
// views has gaps exactly where nobody was looking, which is most of the time."
// Here it is stronger still, because the numbers being watched are destroyed by
// every SFU restart — a page view an hour after an ESS upgrade reads zero and has
// no way to know it is looking at a fresh process.
type Sampler struct {
	store    *Store
	readFn   func(context.Context) (MediaEvidence, bool)
	interval time.Duration
}

// SamplerInterval is the default cadence.
//
// One minute rather than the Watcher's five. The quantity is different: an address
// change is a daily event and five minutes bounds it finely, while a call is a
// minutes-long event and the *live* gauges are only as current as the last read.
// It costs one in-cluster HTTP GET of a ~20 KB metrics body, and it bounds how
// stale "3 Teilnehmer" can be when an operator looks at the page.
//
// It does not bound the accuracy of the totals. Both counters behind those are
// cumulative, so a call that starts and ends between two samples is still counted —
// only its timing is lost to the gap.
const SamplerInterval = time.Minute

func NewSampler(store *Store, readFn func(context.Context) (MediaEvidence, bool), interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = SamplerInterval
	}
	return &Sampler{store: store, readFn: readFn, interval: interval}
}

// Start runs until ctx is cancelled, sampling once immediately so a fresh install
// has a data point before the first tick.
func (s *Sampler) Start(ctx context.Context) {
	if s == nil || s.store == nil || s.readFn == nil {
		return
	}
	s.sample(ctx)

	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample(ctx)
		}
	}
}

func (s *Sampler) sample(ctx context.Context) {
	ev, ok := s.readFn(ctx)
	if !ok {
		// Nothing recorded. A metrics endpoint that cannot be reached is not an SFU
		// with zero rooms, and writing a zero sample would put a fabricated quiet
		// period into a history whose whole purpose is to be believed. It would also
		// read as a counter reset on the next successful sample, inventing a restart
		// that never happened.
		return
	}

	prev, err := s.store.LatestCounters(ctx)
	if err != nil {
		log.Printf("rtc sampler: could not read previous counters: %v", err)
		return
	}

	if err := s.store.RecordSample(ctx, NewSample(time.Now(), ev, prev)); err != nil {
		log.Printf("rtc sampler: could not record sample: %v", err)
	}
}
