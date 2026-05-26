package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	db     *pgxpool.Pool
	runner *Runner
}

func NewEngine(db *pgxpool.Pool, runner *Runner) *Engine {
	return &Engine{db: db, runner: runner}
}

// RunTrigger executes all enabled hooks matching the trigger, in priority order.
// Returns the IDs of all hook runs started.
func (e *Engine) RunTrigger(ctx context.Context, trigger TriggerType, triggerRef, userID string) ([]uuid.UUID, error) {
	hooks, err := e.listByTrigger(ctx, trigger)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}

	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].Priority < hooks[j].Priority
	})

	var runIDs []uuid.UUID
	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		runID, err := e.runHook(ctx, h, trigger, triggerRef, userID)
		if err != nil {
			log.Printf("hook %s (%s) failed: %v", h.Name, h.ID, err)
		}
		runIDs = append(runIDs, runID)
	}
	return runIDs, nil
}

// RunHookByID executes a specific hook (for manual trigger).
func (e *Engine) RunHookByID(ctx context.Context, hookID uuid.UUID, userID string) (uuid.UUID, error) {
	h, err := e.get(ctx, hookID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("get hook: %w", err)
	}
	return e.runHook(ctx, *h, TriggerManual, "", userID)
}

func (e *Engine) runHook(ctx context.Context, h Hook, trigger TriggerType, triggerRef, userID string) (uuid.UUID, error) {
	runID := uuid.New()
	now := time.Now()

	_, err := e.db.Exec(ctx, `
		INSERT INTO hook_run_log(id, hook_id, trigger_type, trigger_ref, status, triggered_by, ts_start)
		VALUES($1, $2, $3, $4, 'running', $5, $6)`,
		runID, h.ID, trigger, triggerRef, userID, now,
	)
	if err != nil {
		return uuid.Nil, err
	}

	var results []ActionResult
	finalStatus := RunSuccess

	for i, action := range h.Actions {
		res := e.runner.Run(ctx, action)
		res.ActionIndex = i
		results = append(results, res)

		if res.Status == "failed" {
			finalStatus = RunFailed
			break
		}
	}

	if len(results) > 0 && finalStatus == RunFailed {
		// Partial success: some actions ran before failure
		if results[0].Status == "success" {
			finalStatus = RunPartial
		}
	}

	resultsJSON, _ := json.Marshal(results)
	tsEnd := time.Now()
	_, err = e.db.Exec(ctx, `
		UPDATE hook_run_log
		SET status=$1, action_results=$2, ts_end=$3
		WHERE id=$4`,
		finalStatus, resultsJSON, tsEnd, runID,
	)
	return runID, err
}

func (e *Engine) listByTrigger(ctx context.Context, trigger TriggerType) ([]Hook, error) {
	rows, err := e.db.Query(ctx, `
		SELECT id, name, description, trigger, enabled, priority, actions, builtin, created_at, updated_at, created_by
		FROM hooks WHERE trigger=$1 AND enabled=TRUE ORDER BY priority`, trigger)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHooks(rows)
}

func (e *Engine) get(ctx context.Context, id uuid.UUID) (*Hook, error) {
	row := e.db.QueryRow(ctx, `
		SELECT id, name, description, trigger, enabled, priority, actions, builtin, created_at, updated_at, created_by
		FROM hooks WHERE id=$1`, id)
	return scanHook(row)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanHook(row scannable) (*Hook, error) {
	var h Hook
	var actionsJSON []byte
	err := row.Scan(&h.ID, &h.Name, &h.Description, &h.Trigger, &h.Enabled,
		&h.Priority, &actionsJSON, &h.Builtin, &h.CreatedAt, &h.UpdatedAt, &h.CreatedBy)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(actionsJSON, &h.Actions)
	return &h, nil
}

type rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

func scanHooks(rs rows) ([]Hook, error) {
	var result []Hook
	for rs.Next() {
		var h Hook
		var actionsJSON []byte
		err := rs.Scan(&h.ID, &h.Name, &h.Description, &h.Trigger, &h.Enabled,
			&h.Priority, &actionsJSON, &h.Builtin, &h.CreatedAt, &h.UpdatedAt, &h.CreatedBy)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(actionsJSON, &h.Actions)
		result = append(result, h)
	}
	return result, rs.Err()
}
