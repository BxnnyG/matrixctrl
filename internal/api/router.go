package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bxnny/matrixctrl/internal/api/handlers"
	authmw "github.com/bxnny/matrixctrl/internal/api/middleware"
)

type Deps struct {
	Auth   *handlers.AuthHandler
	Status *handlers.StatusHandler
	Hooks  *handlers.HooksHandler
	Helm   *handlers.HelmHandler
	WS     *handlers.WSHandler
	Config *handlers.ConfigHandler
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Public auth routes
	r.Route("/api/v1/auth", func(r chi.Router) {
		// Bootstrap login only available when OIDC is not configured
		if !deps.Auth.OIDCConfigured() {
			r.Post("/bootstrap/login", deps.Auth.BootstrapLogin)
		}
		r.Get("/oidc/available", deps.Auth.OIDCAvailable)
		r.Get("/oidc/redirect", deps.Auth.OIDCRedirect)
		r.Get("/oidc/callback", deps.Auth.OIDCCallback)
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(authmw.RequireAuth(deps.Auth.ValidateToken))

		r.Post("/api/v1/auth/logout", deps.Auth.Logout)
		r.Get("/api/v1/auth/me", deps.Auth.Me)

		r.Route("/api/v1/status", func(r chi.Router) {
			r.Get("/", deps.Status.Get)
			r.Get("/components", deps.Status.Components)
			r.Get("/release", deps.Status.Release)
			r.Delete("/evicted-pods", deps.Status.DeleteEvictedPods)
			r.Get("/sysinfo", deps.Status.SysInfo)
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
			r.Post("/{id}/trigger", deps.Hooks.Trigger)
			r.Get("/{id}/runs", deps.Hooks.ListRuns)
			r.Get("/{id}/runs/{runId}", deps.Hooks.GetRun)
		})

		r.Route("/api/v1/config", func(r chi.Router) {
			r.Get("/slices", deps.Config.ListSlices)
			r.Get("/slices/{name}", deps.Config.GetSlice)
			r.Put("/slices/{name}", deps.Config.PutSlice)
			r.Get("/merged", deps.Config.GetMerged)
			r.Post("/validate", deps.Config.Validate)
			r.Post("/validate-merged", deps.Config.ValidateMerged)
			r.Get("/schema", deps.Config.GetSchema)
			r.Get("/easy", deps.Config.GetEasy)
			r.Post("/easy", deps.Config.PutEasy)
			r.Get("/diff", deps.Config.GetDiff)
			r.Post("/apply", deps.Config.Apply)
			r.Get("/history", deps.Config.GetHistory)
			r.Get("/history/{sha}/diff", deps.Config.GetCommitDiff)
			r.Post("/history/{sha}/rollback", deps.Config.RollbackToCommit)
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
