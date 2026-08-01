package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// AuditSink writes one audit record. An implementation must not block the
// request: Audit calls it after the response has been written.
type AuditSink interface {
	Write(ctx context.Context, rec AuditRecord) error
}

// AuditRecord is one attempted change.
//
// Note what is *not* in here: no request body, no headers, no query string.
// Logging the payload would put MAS client secrets into the audit table the
// first time someone connects OIDC, and arbitrary config YAML on every save. An
// audit trail that becomes a second copy of the secrets is a liability, not a
// control — and "what exactly changed" is answered better by the config repo's
// git history, which stores diffs and can roll back.
type AuditRecord struct {
	UserID   string
	Action   string // "POST /api/v1/helm/releases/{name}/upgrade"
	Resource string // the concrete path
	Status   int
	Duration time.Duration
	Result   string // "ok" | "error"
}

// auditedMethods are the ones that can change something. GET and HEAD are left
// out on purpose: logging every dashboard poll would bury the eight entries a
// month that actually matter under thousands that do not.
var auditedMethods = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Audit records every mutating request that passes through it.
//
// This is middleware rather than a call in each handler on purpose. Per-handler
// logging is a rule enforced by memory, and this project has been bitten by that
// repeatedly — the release nobody remembered to publish, the gofmt drift, the
// hostname in a plan document. A new handler must not be *able* to be forgotten:
// route it through here and it is covered.
//
// Failures are logged and swallowed. An audit write must never turn a successful
// upgrade into a failed HTTP response — but it must also never fail silently, or
// the trail develops holes nobody knows about.
func Audit(sink AuditSink) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sink == nil || !auditedMethods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}

			rec := &statusRecorder{ResponseWriter: w}
			started := time.Now()
			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK // handler returned without writing
			}

			result := "ok"
			if status >= 400 {
				// Deliberately recorded, not dropped. A rejected upgrade and a
				// failed login are exactly what an audit trail is for; one that
				// lists only successes is worse than none.
				result = "error"
			}

			// The route pattern, not the concrete URL: patterns aggregate
			// ("who upgrades releases?"), URLs do not.
			pattern := r.URL.Path
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				pattern = rc.RoutePattern()
			}

			// The request context is cancelled once the response is done, so the
			// write gets its own — with a bound, since a hanging audit write
			// must not leak a goroutine per request.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()

			err := sink.Write(ctx, AuditRecord{
				UserID:   UserIDFromContext(r.Context()),
				Action:   r.Method + " " + strings.TrimSuffix(pattern, "/"),
				Resource: r.URL.Path,
				Status:   status,
				Duration: time.Since(started),
				Result:   result,
			})
			if err != nil {
				log.Printf("audit: could not record %s %s: %v", r.Method, r.URL.Path, err)
			}
		})
	}
}
