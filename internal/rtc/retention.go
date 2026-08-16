package rtc

import (
	"context"
	"log"
	"time"
)

// Bounding the two telemetry tables (etappe 45, P2-19's class).
//
// Both grow forever by design: rtc_samples adds 1440 rows a day, and
// rtc_address_history one per genuine address change. On a single-node cluster
// sharing its disk with Synapse's database and media, unbounded append-only is a
// real operational trap.
//
// This is deliberately *not* the decision P2-19 refuses to guess at. That entry is
// about the audit log, which answers "who did what" — its retention is a compliance
// question and a default invented by whoever wrote the INSERT is the wrong way to
// settle it. These two tables are operational telemetry with no such duty: nobody
// needs last spring's participant count to answer for anything. So a default is
// appropriate, and the number lives here rather than in a migration nobody rereads.

const (
	// SampleRetention keeps enough to answer "what happened recently" at full
	// resolution. Ninety days of minute samples is ~130k rows and a few megabytes —
	// small enough to be free, long enough that a quarterly question is answerable.
	//
	// Deliberately not a downsampling scheme. Keeping hourly averages beyond this
	// would be a second representation of the same data, with its own bugs, to save
	// a few megabytes on a disk measured in gigabytes.
	SampleRetention = 90 * 24 * time.Hour

	// AddressRetention is longer because the rows are rarer and each one is an
	// event an operator may want to correlate with a much older incident. One row
	// per real address change is a handful a month on a dynamic line.
	AddressRetention = 365 * 24 * time.Hour

	// pruneInterval is how often the delete runs. Daily: the tables are bounded by
	// the retention window, not by how promptly it is enforced, so anything more
	// frequent is work for its own sake.
	pruneInterval = 24 * time.Hour
)

// Prune deletes observations past their retention. Returns rows removed.
func (s *Store) Prune(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var total int64

	tag, err := s.db.Exec(ctx,
		`DELETE FROM rtc_samples WHERE observed_at < $1`, time.Now().Add(-SampleRetention))
	if err != nil {
		return total, err
	}
	total += tag.RowsAffected()

	// last_seen, not first_seen: a run that started two years ago and is *still* the
	// current address must not be deleted, or the page would lose the answer to
	// "since when has this been stable" and AssessFreshness would fall back to
	// Unknown on the most stable deployment there is.
	tag, err = s.db.Exec(ctx,
		`DELETE FROM rtc_address_history WHERE last_seen < $1`, time.Now().Add(-AddressRetention))
	if err != nil {
		return total, err
	}
	return total + tag.RowsAffected(), nil
}

// StartPruning runs Prune on a timer until ctx is cancelled.
//
// The first run is delayed rather than immediate. Startup is the busiest moment a
// process has — migrations, OIDC registration, the first cluster reads — and a
// delete that can wait a minute should not compete with any of it.
func (s *Store) StartPruning(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	t := time.NewTicker(pruneInterval)
	defer t.Stop()

	first := time.NewTimer(time.Minute)
	defer first.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.prune(ctx)
		case <-t.C:
			s.prune(ctx)
		}
	}
}

func (s *Store) prune(ctx context.Context) {
	n, err := s.Prune(ctx)
	if err != nil {
		log.Printf("rtc: could not prune telemetry: %v", err)
		return
	}
	if n > 0 {
		log.Printf("rtc: pruned %d telemetry rows past retention", n)
	}
}
