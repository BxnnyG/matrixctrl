package synapse

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Where a report stands, according to this panel. See migrations/013 for why it is
// not stored in Synapse.

const (
	StateOpen      = "open"
	StateHandled   = "handled"
	StateDismissed = "dismissed"
)

// Disposition is an admin's decision about one report.
type Disposition struct {
	State     string    `json:"state"`
	Note      string    `json:"note,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	DecidedAt time.Time `json:"decided_at,omitempty"`
}

// Dispositions stores them.
type Dispositions struct{ db *pgxpool.Pool }

func NewDispositions(db *pgxpool.Pool) *Dispositions { return &Dispositions{db: db} }

// ValidState reports whether a state may be written. `open` is deliberately not
// writable: it is the *absence* of a decision, expressed by the absence of a row, so
// accepting it as a value would create two ways to say the same thing that could
// then disagree.
func ValidState(s string) bool { return s == StateHandled || s == StateDismissed }

// For returns the dispositions for a set of report ids, keyed by id.
//
// One query for the page rather than one per row: a fifty-row queue would otherwise
// be fifty round trips to render a badge.
func (d *Dispositions) For(ctx context.Context, ids []int64) (map[int64]Disposition, error) {
	out := map[int64]Disposition{}
	if d == nil || d.db == nil || len(ids) == 0 {
		return out, nil
	}
	rows, err := d.db.Query(ctx,
		`SELECT report_id, state, note, COALESCE(actor, ''), decided_at
		 FROM event_report_dispositions WHERE report_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var disp Disposition
		if err := rows.Scan(&id, &disp.State, &disp.Note, &disp.Actor, &disp.DecidedAt); err != nil {
			return nil, err
		}
		out[id] = disp
	}
	return out, rows.Err()
}

// Set records a decision, replacing any previous one.
func (d *Dispositions) Set(ctx context.Context, reportID int64, state, note, actor string) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("no database")
	}
	if !ValidState(state) {
		return fmt.Errorf("invalid state %q", state)
	}
	_, err := d.db.Exec(ctx, `
		INSERT INTO event_report_dispositions (report_id, state, note, actor, decided_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), now())
		ON CONFLICT (report_id) DO UPDATE
		SET state = EXCLUDED.state, note = EXCLUDED.note,
		    actor = EXCLUDED.actor, decided_at = EXCLUDED.decided_at`,
		reportID, state, note, actor)
	return err
}

// Reopen removes a decision, putting the report back on the open queue.
//
// A delete rather than a third state: "open" is the absence of a decision, and
// storing a row that says so would make the queue depend on which of two
// representations a reader checked.
func (d *Dispositions) Reopen(ctx context.Context, reportID int64) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("no database")
	}
	_, err := d.db.Exec(ctx,
		`DELETE FROM event_report_dispositions WHERE report_id = $1`, reportID)
	return err
}

// OpenCount counts reports with no decision, given the ids currently known upstream.
//
// Takes the upstream ids rather than counting rows here, because this table only
// knows about reports somebody has decided on: the open ones are precisely the ones
// it has never heard of.
func (d *Dispositions) OpenCount(ctx context.Context, upstreamIDs []int64) (int, error) {
	if len(upstreamIDs) == 0 {
		return 0, nil
	}
	decided, err := d.For(ctx, upstreamIDs)
	if err != nil {
		return 0, err
	}
	return len(upstreamIDs) - len(decided), nil
}
