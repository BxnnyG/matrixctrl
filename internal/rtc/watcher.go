package rtc

import (
	"context"
	"log"
	"net"
	"time"
)

// Watcher records what the announced RTC host resolves to, on a timer.
//
// Without it the observations would only be written when someone opens the Calls
// page — which is precisely when they would notice the problem anyway. The check
// measures *when the address changed*, and that timestamp can only be as accurate
// as the observation interval. A history built from page views has gaps exactly
// where nobody was looking, which is most of the time.
type Watcher struct {
	store    *Store
	hostFn   func(context.Context) string
	interval time.Duration
}

// NewWatcher takes a function rather than a host string because the announced host
// lives in the config store and can change without a restart.
func NewWatcher(store *Store, hostFn func(context.Context) string, interval time.Duration) *Watcher {
	if interval <= 0 {
		// Five minutes bounds the error on "when did it change" to five minutes,
		// which is far finer than the daily cadence being detected, while costing
		// one DNS lookup — usually answered from the resolver's cache.
		interval = 5 * time.Minute
	}
	return &Watcher{store: store, hostFn: hostFn, interval: interval}
}

// Start runs until ctx is cancelled. It observes once immediately so a fresh
// install has a first data point before the first tick, rather than a five-minute
// window in which the page can say nothing at all.
func (w *Watcher) Start(ctx context.Context) {
	if w == nil || w.store == nil || w.hostFn == nil {
		return
	}

	w.observe(ctx)

	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.observe(ctx)
		}
	}
}

func (w *Watcher) observe(ctx context.Context) {
	host := w.hostFn(ctx)
	if host == "" {
		return // not configured yet; nothing to observe and nothing to complain about
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		// Deliberately records nothing. A failed lookup is not an address change,
		// and writing one would make an air-gapped or briefly-offline cluster look
		// like its address moves every five minutes.
		return
	}

	if err := w.store.Record(ctx, host, addrs[0]); err != nil {
		log.Printf("rtc watcher: could not record observation for %q: %v", host, err)
	}
}
