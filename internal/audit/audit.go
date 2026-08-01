// Package audit persists and reads back the record of who changed what.
//
// The HTTP layer decides *what* counts as an auditable event (see
// internal/api/middleware/audit.go); this package only stores and queries it.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
)

type Store struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Write implements middleware.AuditSink.
//
// An unauthenticated request still produces a row — a failed login is exactly
// what an audit trail is for — but it must be attributable to *something*, and
// `user_id` is NOT NULL.
func (s *Store) Write(ctx context.Context, rec authmw.AuditRecord) error {
	userID := rec.UserID
	if userID == "" {
		userID = "-"
	}

	detail, err := json.Marshal(map[string]any{
		"status":      rec.Status,
		"duration_ms": rec.Duration.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("encode detail: %w", err)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO audit_log(user_id, action, resource, detail, result)
		VALUES($1, $2, $3, $4, $5)`,
		userID, rec.Action, rec.Resource, detail, rec.Result,
	)
	if err != nil {
		return fmt.Errorf("insert audit row: %w", err)
	}
	return nil
}

type Entry struct {
	ID       int64          `json:"id"`
	TS       time.Time      `json:"ts"`
	UserID   string         `json:"user_id"`
	Action   string         `json:"action"`
	Resource string         `json:"resource"`
	Result   string         `json:"result"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// Filter narrows a listing. A zero Filter means "everything, newest first".
type Filter struct {
	UserID string
	Result string
	Before int64 // keyset cursor: return entries with id < Before
	Limit  int
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

// List returns entries newest-first, paginated by **keyset** rather than offset.
//
// The table only ever grows at the head, so OFFSET would make every page re-scan
// the rows above it and, worse, shift entries between pages while new ones
// arrive — a paginated audit trail that can skip a row while you read it is not
// an audit trail. `id` is a BIGSERIAL, so id < cursor is a stable "older than".
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, ts, user_id, action, COALESCE(resource, ''), detail, result
		  FROM audit_log
		 WHERE ($1 = '' OR user_id = $1)
		   AND ($2 = '' OR result  = $2)
		   AND ($3 = 0  OR id     < $3)
		 ORDER BY id DESC
		 LIMIT $4`,
		f.UserID, f.Result, f.Before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, limit)
	for rows.Next() {
		var e Entry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.UserID, &e.Action, &e.Resource, &raw, &e.Result); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &e.Detail)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
