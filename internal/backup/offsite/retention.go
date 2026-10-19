package offsite

import (
	"fmt"
	"sort"
	"time"
)

// Policy is how long backups are kept.
type Policy struct {
	// Daily keeps the newest backup of each of the Daily most recent days that have one.
	Daily int
	// Weekly keeps the newest backup of each of the Weekly most recent ISO weeks that
	// have one.
	Weekly int
	// Upgrade is how long a backup taken before a version change is kept. They are
	// outside the daily/weekly rhythm: they exist to undo one particular step.
	Upgrade time.Duration
}

// DefaultPolicy is the operator's decision (etappe 120): 7 daily, 4 weekly, and every
// pre-upgrade backup for 30 days.
var DefaultPolicy = Policy{Daily: 7, Weekly: 4, Upgrade: 30 * 24 * time.Hour}

// Retain splits entries into those to keep and those to delete. A pure function: no
// bucket, no clock but the one passed in, which is why the boundaries can be tested.
//
// Counting "the newest N days that *have* a backup" instead of "the last N calendar
// days" is deliberate. If backups stop for a fortnight, a calendar window would find
// nothing inside it and delete every backup that exists, at the moment they are the only
// ones there are. Counted by occurrence, the newest backup is never a candidate for
// deletion.
//
// Days and weeks are UTC. Daily and weekly sets overlap by design (this week's newest is
// also today's); a backup is kept if any rule keeps it.
func Retain(entries []Entry, now time.Time, p Policy) (keep, drop []Entry) {
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.After(sorted[j].Time) })

	days := map[string]bool{}
	weeks := map[string]bool{}
	for _, e := range sorted {
		if e.IsUpgrade() {
			if now.Sub(e.Time) <= p.Upgrade {
				keep = append(keep, e)
			} else {
				drop = append(drop, e)
			}
			continue
		}
		kept := false
		day := e.Time.UTC().Format("2006-01-02")
		if !days[day] && len(days) < p.Daily {
			days[day] = true
			kept = true
		}
		y, w := e.Time.UTC().ISOWeek()
		week := fmt.Sprintf("%d-%02d", y, w)
		if !weeks[week] && len(weeks) < p.Weekly {
			weeks[week] = true
			kept = true
		}
		if kept {
			keep = append(keep, e)
		} else {
			drop = append(drop, e)
		}
	}
	return keep, drop
}
