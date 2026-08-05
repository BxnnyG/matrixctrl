package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bxnnyg/matrixctrl/internal/api/handlers"
	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
)

type Deps struct {
	Auth   *handlers.AuthHandler
	Status *handlers.StatusHandler
	Hooks  *handlers.HooksHandler
	Helm   *handlers.HelmHandler
	WS     *handlers.WSHandler
	Config *handlers.ConfigHandler
	Setup  *handlers.SetupHandler
	Audit  *handlers.AuditHandler
	RTC    *handlers.RTCHandler
	Drift  *handlers.DriftHandler
	Users  *handlers.UsersHandler

	// AuditSink records every mutating request. Nil disables auditing, which is
	// what the tests use — production always wires it.
	AuditSink authmw.AuditSink
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// No CORS middleware. The frontend is served by this same binary on the same
	// origin, so nothing needs it — and a wildcard Access-Control-Allow-Origin on an
	// admin API is worth removing rather than configuring (P2-26).

	// Public auth routes. Bootstrap login is always registered but the handler
	// refuses once OIDC is active — so we can switch auth modes at runtime.
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/bootstrap/login", deps.Auth.BootstrapLogin)
		r.Get("/oidc/available", deps.Auth.OIDCAvailable)
		r.Get("/oidc/redirect", deps.Auth.OIDCRedirect)
		r.Get("/oidc/callback", deps.Auth.OIDCCallback)
		// Trades the one-time code from the callback fragment for the session
		// token, over a POST whose body is never logged (P0-5).
		r.Post("/exchange", deps.Auth.ExchangeCode)
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(authmw.RequireAuth(deps.Auth.ValidateToken))

		// After RequireAuth, so the record carries the authenticated user, and
		// wrapping the whole group rather than each route: a new handler must
		// not be able to be forgotten (docs/plans/etappe-17-audit-trail.md).
		r.Use(authmw.Audit(deps.AuditSink))

		r.Post("/api/v1/auth/logout", deps.Auth.Logout)
		r.Get("/api/v1/auth/me", deps.Auth.Me)

		if deps.Audit != nil {
			r.Get("/api/v1/audit", deps.Audit.List)
		}
		if deps.RTC != nil {
			r.Get("/api/v1/rtc/status", deps.RTC.Status)
			r.Get("/api/v1/users", deps.Users.List)
		}

		r.Route("/api/v1/status", func(r chi.Router) {
			r.Get("/", deps.Status.Get)
			r.Get("/components", deps.Status.Components)
			r.Get("/release", deps.Status.Release)
			r.Delete("/evicted-pods", deps.Status.DeleteEvictedPods)
			r.Get("/sysinfo", deps.Status.SysInfo)
			r.Get("/events", deps.Status.Events)
			r.Get("/components/{name}/pods", deps.Status.ComponentDetail)
			r.Get("/pods/{deployment}", deps.Status.DeploymentPods)
			r.Get("/pods/{pod}/logs", deps.Status.PodLogs)
			r.Delete("/pods/{pod}", deps.Status.RestartPod)
		})

		r.Route("/api/v1/hooks", func(r chi.Router) {
			r.Get("/", deps.Hooks.List)
			r.Post("/", deps.Hooks.Create)
			r.Get("/{id}", deps.Hooks.Get)
			r.Put("/{id}", deps.Hooks.Update)
			r.Delete("/{id}", deps.Hooks.Delete)
			r.Post("/{id}/enabled", deps.Hooks.SetEnabled)
			r.Post("/{id}/trigger", deps.Hooks.Trigger)
			r.Get("/{id}/runs", deps.Hooks.ListRuns)
			r.Get("/{id}/runs/{runId}", deps.Hooks.GetRun)
		})

		// Reports whether those hooks' patches are still in effect. Separate from
		// /hooks because "enabled" and "applied" are different questions, and
		// conflating them is what let a broken cluster read as green.
		r.Get("/api/v1/drift", deps.Drift.Status)

		// Replacing the SFU pod is the fix for an announced address that went stale
		// overnight. POST so it lands in the audit log like every other mutation.
		// Verb-in-path on purpose: the audit middleware records no request body, so
		// the path is the only place the meaning of the change can live.
		for _, action := range []string{"lock", "unlock", "deactivate", "reactivate", "grant-admin", "revoke-admin", "set-password"} {
			r.Post("/api/v1/users/{id}/"+action, deps.Users.Act(action))
		}

		r.Post("/api/v1/rtc/restart-sfu", deps.RTC.RestartSFU)
		// POST because it discloses the deployment's public address to a third
		// party — see handlers.Reachability. Never reachable by a page load.
		r.Post("/api/v1/rtc/reachability", deps.RTC.Reachability)

		r.Route("/api/v1/config", func(r chi.Router) {
			r.Get("/slices", deps.Config.ListSlices)
			r.Get("/slices/{name}", deps.Config.GetSlice)
			r.Put("/slices/{name}", deps.Config.PutSlice)
			r.Get("/merged", deps.Config.GetMerged)
			r.Post("/validate", deps.Config.Validate)
			r.Post("/validate-merged", deps.Config.ValidateMerged)
			r.Get("/schema", deps.Config.GetSchema)
			r.Get("/settings", deps.Config.GetSettings)
			r.Post("/settings", deps.Config.PutSettings)
			r.Get("/diff", deps.Config.GetDiff)
			r.Post("/apply", deps.Config.Apply)
			r.Get("/history", deps.Config.GetHistory)
			r.Get("/history/{sha}/diff", deps.Config.GetCommitDiff)
			r.Post("/history/{sha}/rollback", deps.Config.RollbackToCommit)
		})

		r.Route("/api/v1/setup", func(r chi.Router) {
			r.Get("/status", deps.Setup.Status)
			r.Get("/discover", deps.Setup.Discover)
			r.Post("/adopt", deps.Setup.Adopt)
			r.Get("/chart-defaults", deps.Setup.ChartDefaults)
			r.Post("/deploy-ess", deps.Helm.DeployESS)
			r.Post("/connect-oidc", deps.Helm.ConnectOIDC)
		})

		r.Route("/api/v1/helm", func(r chi.Router) {
			r.Get("/versions", deps.Helm.ListVersions)
			r.Get("/releases/{name}", deps.Helm.GetRelease)
			r.Get("/releases/{name}/history", deps.Helm.GetHistory)
			r.Post("/releases/{name}/upgrade", deps.Helm.Upgrade)
			r.Post("/releases/{name}/apply-config", deps.Helm.ApplyConfig)
			r.Post("/releases/{name}/rollback", deps.Helm.Rollback)
			r.Get("/releases/{name}/upgrade/{upgradeId}", deps.Helm.GetUpgradeStatus)
			r.HandleFunc("/releases/{name}/upgrade/{upgradeId}/logs", deps.WS.UpgradeLogs)
		})
	})

	// Serve embedded frontend for all other routes
	r.NotFound(deps.Status.ServeFrontend)

	return r
}
