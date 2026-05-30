package hooks

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

type Runner struct {
	k8s *k8s.Client
}

func NewRunner(k8sClient *k8s.Client) *Runner {
	return &Runner{k8s: k8sClient}
}

func (r *Runner) Run(ctx context.Context, action HookAction) ActionResult {
	start := time.Now()
	result := ActionResult{
		Type:   string(action.Type),
		Status: "success",
	}

	var err error
	switch action.Type {
	case ActionKubectlPatch:
		err = r.runPatch(ctx, action)
	case ActionWaitRollout:
		err = r.runWaitRollout(ctx, action)
	case ActionHTTPRequest:
		err = r.runHTTP(ctx, action)
	default:
		err = fmt.Errorf("unknown action type: %s", action.Type)
	}

	result.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	}
	return result
}

func (r *Runner) runPatch(ctx context.Context, action HookAction) error {
	var pt types.PatchType
	switch action.PatchType {
	case "json":
		pt = types.JSONPatchType
	case "strategic":
		pt = types.StrategicMergePatchType
	default:
		pt = types.MergePatchType
	}

	ns := action.Namespace
	if ns == "" {
		ns = "ess"
	}

	return r.k8s.Patch(ctx, action.Resource, ns, action.Name, pt, []byte(action.Patch))
}

func (r *Runner) runWaitRollout(ctx context.Context, action HookAction) error {
	timeout := 120 * time.Second
	if action.TimeoutSecs > 0 {
		timeout = time.Duration(action.TimeoutSecs) * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ns := action.Namespace
	if ns == "" {
		ns = "ess"
	}

	return r.k8s.WaitForRollout(waitCtx, ns, action.Name)
}

func (r *Runner) runHTTP(_ context.Context, _ HookAction) error {
	return fmt.Errorf("http_request actions not yet implemented (Phase 1)")
}
