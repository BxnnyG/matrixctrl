package hooks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// httpActionTimeout bounds one request.
//
// Hooks run inside an upgrade's hook phase, so a request that hangs holds up the
// operation an operator is watching. Fifteen seconds is generous for a notification and
// short enough that a dead endpoint is reported rather than waited on.
const httpActionTimeout = 15 * time.Second

// runHTTP performs the action's request (etappe 62).
//
// Declared, offered in the hook editor with a full form, and unimplemented until now:
// saving worked and the hook failed the first time it ran, which is during an upgrade.
//
// Deliberately narrow. One request, no retries — a hook that failed is reported and can
// be re-run by hand, while a silent retry hides a broken endpoint. No headers, so there
// is no field in which a secret would end up stored in plain text in the hooks table.
func (r *Runner) runHTTP(ctx context.Context, action HookAction) error {
	if action.URL == "" {
		return fmt.Errorf("http_request action has no URL")
	}
	method := strings.ToUpper(strings.TrimSpace(action.Method))
	if method == "" {
		method = http.MethodPost
	}

	reqCtx, cancel := context.WithTimeout(ctx, httpActionTimeout)
	defer cancel()

	var body io.Reader
	if action.Body != "" {
		body = strings.NewReader(action.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, action.URL, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	// Only when there is one: a Content-Type on an empty GET is noise, and guessing a
	// more specific type than the operator wrote would be worse than none.
	if action.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, action.URL, err)
	}
	defer resp.Body.Close()
	// Read and discard, bounded: leaving the body unread keeps the connection from
	// being reused, and reading it unbounded lets a misconfigured URL return a
	// gigabyte into a hook run.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	// A notification that silently 404s is not a notification. The status is in the
	// error because it is the one thing that says what to fix.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: HTTP %d", method, action.URL, resp.StatusCode)
	}
	return nil
}
