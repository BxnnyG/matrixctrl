package hooks

import (
	"time"

	"github.com/google/uuid"
)

type TriggerType string

const (
	TriggerPostUpgrade  TriggerType = "post-upgrade"
	TriggerPostRollback TriggerType = "post-rollback"
	TriggerManual       TriggerType = "manual"
)

type ActionType string

const (
	ActionKubectlPatch  ActionType = "kubectl_patch"
	ActionWaitRollout   ActionType = "wait_rollout"
	ActionHTTPRequest   ActionType = "http_request"
)

type HookAction struct {
	Type        ActionType `json:"type"`
	Description string     `json:"description,omitempty"`

	// For kubectl_patch
	Resource  string `json:"resource,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	PatchType string `json:"patch_type,omitempty"` // "json" | "merge" | "strategic"
	Patch     string `json:"patch,omitempty"`

	// For wait_rollout
	TimeoutSecs int `json:"timeout_secs,omitempty"`

	// For http_request
	URL    string `json:"url,omitempty"`
	Method string `json:"method,omitempty"`
	Body   string `json:"body,omitempty"`
}

type Hook struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Trigger     TriggerType `json:"trigger"`
	Enabled     bool        `json:"enabled"`
	Priority    int         `json:"priority"`
	Actions     []HookAction `json:"actions"`
	Builtin     bool        `json:"builtin"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	CreatedBy   string      `json:"created_by"`

	// Populated from last run (not stored in hooks table)
	LastRunStatus string `json:"lastRunStatus,omitempty"`
}

type ActionResult struct {
	ActionIndex int    `json:"action_index"`
	Type        string `json:"type"`
	Status      string `json:"status"` // "success" | "failed"
	Error       string `json:"error,omitempty"`
	DurationMs  int64  `json:"duration_ms"`
}

type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunSuccess RunStatus = "success"
	RunFailed  RunStatus = "failed"
	RunPartial RunStatus = "partial"
)

type HookRun struct {
	ID            uuid.UUID      `json:"id"`
	HookID        uuid.UUID      `json:"hook_id"`
	TsStart       time.Time      `json:"ts_start"`
	TsEnd         *time.Time     `json:"ts_end,omitempty"`
	TriggerType   TriggerType    `json:"trigger_type"`
	TriggerRef    string         `json:"trigger_ref,omitempty"`
	Status        RunStatus      `json:"status"`
	ActionResults []ActionResult `json:"action_results"`
	TriggeredBy   string         `json:"triggered_by"`
}
