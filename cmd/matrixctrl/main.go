package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bxnny/matrixctrl/internal/api"
	"github.com/bxnny/matrixctrl/internal/api/handlers"
	"github.com/bxnny/matrixctrl/internal/auth"
	"github.com/bxnny/matrixctrl/internal/config"
	"github.com/bxnny/matrixctrl/internal/db"
	gitpkg "github.com/bxnny/matrixctrl/internal/git"
	"github.com/bxnny/matrixctrl/internal/helm"
	"github.com/bxnny/matrixctrl/internal/hooks"
	builtin "github.com/bxnny/matrixctrl/internal/hooks/builtin"
	"github.com/bxnny/matrixctrl/internal/k8s"
	"github.com/bxnny/matrixctrl/internal/server"
	"github.com/bxnny/matrixctrl/internal/version"
)

func main() {
	log.Printf("MatrixCtrl %s (%s) starting", version.Version, version.Commit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbURL := env("MATRIXCTRL_DB_URL", "postgres://matrixctrl:dev@localhost:5432/matrixctrl?sslmode=disable")
	addr := env("MATRIXCTRL_ADDR", ":8080")
	essNS := env("MATRIXCTRL_ESS_NAMESPACE", "ess")
	essRelease := env("MATRIXCTRL_ESS_RELEASE", "ess")
	configRepoPath := env("MATRIXCTRL_CONFIG_REPO", "/data/config-repo")
	configSeedPath := env("MATRIXCTRL_CONFIG_SEED", "/root/ess-config-values")

	pool, err := db.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	bootstrapAuth := auth.NewBootstrap(ctx, pool)
	if err := bootstrapAuth.EnsureAdminExists(ctx); err != nil {
		log.Printf("warning: bootstrap admin: %v", err)
	}

	// OIDC — optional; only wired when env vars are set.
	oidcCfg := auth.OIDCConfig{
		ClientID:     env("MATRIXCTRL_OIDC_CLIENT_ID", ""),
		ClientSecret: env("MATRIXCTRL_OIDC_CLIENT_SECRET", ""),
		Issuer:       env("MATRIXCTRL_OIDC_ISSUER", ""),
		RedirectURI:  env("MATRIXCTRL_OIDC_REDIRECT_URI", ""),
		RequireAdmin: env("MATRIXCTRL_REQUIRE_ADMIN", "true") != "false", // admin-only by default
	}
	if allowed := env("MATRIXCTRL_OIDC_ALLOWED_USERS", ""); allowed != "" {
		oidcCfg.AllowedUsers = strings.Split(allowed, ",")
	}
	// Safety net: if OIDC is on with no restriction at all, warn loudly
	if oidcCfg.ClientID != "" && len(oidcCfg.AllowedUsers) == 0 && !oidcCfg.RequireAdmin {
		log.Printf("WARNING: OIDC enabled with no access restriction — any authenticated user can log in!")
		log.Printf("WARNING: Set MATRIXCTRL_REQUIRE_ADMIN=true (default) or MATRIXCTRL_OIDC_ALLOWED_USERS=<ulid>")
	}
	var oidcSvc *auth.OIDCService
	if oidcCfg.ClientID != "" {
		svc, err := auth.NewOIDCService(oidcCfg, pool, bootstrapAuth.JWTKey())
		if err != nil {
			log.Fatalf("OIDC init: %v", err)
		}
		oidcSvc = svc
		log.Printf("OIDC enabled: issuer=%s client_id=%s", oidcCfg.Issuer, oidcCfg.ClientID)
	}

	if err := builtin.Seed(ctx, pool); err != nil {
		log.Printf("warning: seed hooks: %v", err)
	}

	k8sClient, err := k8s.New()
	if err != nil {
		log.Printf("warning: k8s unavailable (dev mode): %v", err)
		k8sClient = nil
	}

	helmClient, err := helm.New(essNS)
	if err != nil {
		log.Printf("warning: helm unavailable: %v", err)
		helmClient = nil
	}

	var runner *hooks.Runner
	if k8sClient != nil {
		runner = hooks.NewRunner(k8sClient)
	}
	engine := hooks.NewEngine(pool, runner)

	configGit, err := gitpkg.OpenOrInit(configRepoPath)
	if err != nil {
		log.Fatalf("config repo: %v", err)
	}
	configStore := config.NewStore(configRepoPath, configGit)
	if err := configStore.Init(ctx, configSeedPath); err != nil {
		log.Printf("warning: config repo init: %v", err)
	}
	if err := configStore.MigrateToSections(ctx); err != nil {
		log.Printf("warning: config section migration: %v", err)
	}

	frontendFS := staticHandler(webDist)

	// Determine current ESS chart version for schema selection
	essVersion := ""
	if helmClient != nil {
		if rel, err := helmClient.GetRelease(essRelease); err == nil {
			essVersion = rel.Version // semver only, e.g. "26.5.1"
		}
	}
	if essVersion == "" {
		essVersion = "26.5.1" // fallback default
	}

	authHandler := handlers.NewAuthHandler(bootstrapAuth, oidcSvc)
	statusHandler := handlers.NewStatusHandler(k8sClient, helmClient, essNS, essRelease, frontendFS)
	hooksHandler := handlers.NewHooksHandler(pool, engine)
	helmHandler := handlers.NewHelmHandler(helmClient, pool, engine, essRelease, configStore)
	wsHandler := handlers.NewWSHandler(helmHandler)
	configHandler := handlers.NewConfigHandler(configStore, configGit, essVersion)
	setupHandler := handlers.NewSetupHandler(helmClient, configStore, essRelease, essNS, oidcSvc != nil && oidcSvc.Enabled())

	router := api.NewRouter(api.Deps{
		Auth:   authHandler,
		Status: statusHandler,
		Hooks:  hooksHandler,
		Helm:   helmHandler,
		WS:     wsHandler,
		Config: configHandler,
		Setup:  setupHandler,
	})

	srv := server.New(addr, router)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.Start(); err != nil {
			log.Printf("server: %v", err)
		}
	}()

	<-sigCh
	log.Printf("shutting down...")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func staticHandler(f fs.FS) http.Handler {
	sub, err := fs.Sub(f, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file; fall back to index.html for SPA client-side routes.
		_, statErr := fs.Stat(sub, r.URL.Path[1:])
		if r.URL.Path == "/" || statErr == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Serve index.html for unknown paths so the SPA router can handle them.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
