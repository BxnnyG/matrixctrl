package rtc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Persistence for the SFU observations. See migrations/011 for why they are kept:
// LiveKit's counters are process-lifetime and the SFU is restarted by the
// post-upgrade hook on every ESS upgrade, so an unrecorded history is no history.

// LatestCounters returns the counters from the most recent sample, or nil when
// none has been taken.
//
// The counters rather than the whole sample, because that is all NewSample needs to
// resolve the next delta, and returning less makes it harder to accidentally treat
// an old gauge reading as current.
func (s *Store) LatestCounters(ctx context.Context) (*Counters, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var c Counters
	err := s.db.QueryRow(ctx, `
		SELECT rooms_completed, room_seconds, quality_samples
		FROM rtc_samples ORDER BY observed_at DESC, id DESC LIMIT 1`).
		Scan(&c.RoomsCompleted, &c.RoomSeconds, &c.QualitySamples)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RecordSample writes one observation.
func (s *Store) RecordSample(ctx context.Context, smp Sample) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO rtc_samples (
			observed_at, rooms_live, participants_live,
			rooms_completed, room_seconds, quality_samples,
			d_rooms_completed, d_room_seconds, d_quality_samples, sfu_restarted)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		smp.ObservedAt, smp.RoomsLive, smp.ParticipantsLive,
		smp.RoomsCompleted, smp.RoomSeconds, smp.QualitySamples,
		smp.DRoomsCompleted, smp.DRoomSeconds, smp.DQualitySamples, smp.SFURestarted)
	return err
}

// SamplesSince returns the observations in a window, oldest first.
//
// Bounded, like History above. One sample a minute is 1440 a day, so an unbounded
// SELECT over a year is 500k rows into a page — the kind of query that is fine for
// a month and then is not.
func (s *Store) SamplesSince(ctx context.Context, since time.Time, limit int) ([]Sample, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 20000 {
		limit = 20000
	}
	rows, err := s.db.Query(ctx, `
		SELECT observed_at, rooms_live, participants_live,
		       rooms_completed, room_seconds, quality_samples,
		       d_rooms_completed, d_room_seconds, d_quality_samples, sfu_restarted
		FROM rtc_samples WHERE observed_at >= $1
		ORDER BY observed_at ASC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var smp Sample
		if err := rows.Scan(&smp.ObservedAt, &smp.RoomsLive, &smp.ParticipantsLive,
			&smp.RoomsCompleted, &smp.RoomSeconds, &smp.QualitySamples,
			&smp.DRoomsCompleted, &smp.DRoomSeconds, &smp.DQualitySamples, &smp.SFURestarted); err != nil {
			return nil, err
		}
		out = append(out, smp)
	}
	return out, rows.Err()
}

// DailyTotal is one day's activity.
type DailyTotal struct {
	Day      time.Time `json:"day"`
	Calls    int       `json:"calls"`
	Seconds  int       `json:"seconds"`
	Restarts int       `json:"sfu_restarts"`
}

// Daily aggregates the resolved deltas by day.
//
// Summing in the database rather than in Go: the alternative is shipping every
// sample to the process to add them up, which is the same answer and 1440 rows a
// day of network for it.
func (s *Store) Daily(ctx context.Context, days int) ([]DailyTotal, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	rows, err := s.db.Query(ctx, `
		SELECT date_trunc('day', observed_at) AS day,
		       COALESCE(sum(d_rooms_completed), 0),
		       COALESCE(sum(d_room_seconds), 0),
		       COALESCE(count(*) FILTER (WHERE sfu_restarted), 0)
		FROM rtc_samples
		WHERE observed_at >= now() - make_interval(days => $1)
		GROUP BY day ORDER BY day ASC`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DailyTotal
	for rows.Next() {
		var d DailyTotal
		if err := rows.Scan(&d.Day, &d.Calls, &d.Seconds, &d.Restarts); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
