package rtc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store records what the announced RTC host resolves to, over time. See
// migrations/007 for why the history is kept rather than just the current value:
// the *moment of change* is the thing being measured, and a single current value
// cannot carry it.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Newest returns the latest observation for a host, or nil if the host has never
// been resolved. The Changes count comes back with it because a single observation
// means "nothing has changed yet", which must not be read as "the address is
// current" (see AssessFreshness).
func (s *Store) Newest(ctx context.Context, host string) (*AddressObservation, error) {
	if s == nil || s.db == nil || host == "" {
		return nil, nil
	}

	var obs AddressObservation
	err := s.db.QueryRow(ctx, `
		SELECT address, first_seen, last_seen,
		       (SELECT count(*) FROM rtc_address_history WHERE host = $1)
		FROM rtc_address_history
		WHERE host = $1
		ORDER BY first_seen DESC
		LIMIT 1`, host).Scan(&obs.Address, &obs.FirstSeen, &obs.LastSeen, &obs.Changes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &obs, nil
}

// Record files one resolution. It is a transcription of NextObservation, which is
// where the decision actually lives so it can be tested without a database.
//
// Failures are returned rather than swallowed, but callers on a read path should
// log and continue: not being able to record an observation is a worse reason to
// fail a page than it is to lose one data point.
func (s *Store) Record(ctx context.Context, host, resolved string) error {
	if s == nil || s.db == nil || host == "" {
		return nil
	}

	newest, err := s.Newest(ctx, host)
	if err != nil {
		return err
	}

	switch NextObservation(resolved, newest) {
	case ActionSkip:
		return nil
	case ActionExtend:
		_, err = s.db.Exec(ctx, `
			UPDATE rtc_address_history SET last_seen = NOW()
			WHERE id = (SELECT id FROM rtc_address_history WHERE host = $1
			            ORDER BY first_seen DESC LIMIT 1)`, host)
		return err
	default: // ActionInsert
		_, err = s.db.Exec(ctx,
			`INSERT INTO rtc_address_history (host, address) VALUES ($1, $2)`, host, resolved)
		return err
	}
}

// History returns the recorded runs for a host, newest first. Bounded because this
// table grows by one row per address change and an operator on a re-addressed line
// accumulates roughly one per day — a year of that is still small, but an unbounded
// SELECT is how a page gets slow two years after anyone looks at this code.
func (s *Store) History(ctx context.Context, host string, limit int) ([]AddressObservation, error) {
	if s == nil || s.db == nil || host == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := s.db.Query(ctx, `
		SELECT address, first_seen, last_seen FROM rtc_address_history
		WHERE host = $1 ORDER BY first_seen DESC LIMIT $2`, host, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AddressObservation
	for rows.Next() {
		var o AddressObservation
		if err := rows.Scan(&o.Address, &o.FirstSeen, &o.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ParsePodStart turns the string PodInfo carries into a time. An unparseable or
// absent value yields the zero time, which AssessFreshness reports as unknown
// rather than guessing.
func ParsePodStart(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
