package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/api"
	"github.com/bxnnyg/matrixctrl/internal/api/handlers"
	"github.com/bxnnyg/matrixctrl/internal/audit"
	"github.com/bxnnyg/matrixctrl/internal/auth"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/db"
	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
	builtin "github.com/bxnnyg/matrixctrl/internal/hooks/builtin"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/rtc"
	"github.com/bxnnyg/matrixctrl/internal/server"
	"github.com/bxnnyg/matrixctrl/internal/synapse"
	"github.com/bxnnyg/matrixctrl/internal/version"
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

	// Startup is the one moment where "no process owns this upgrade" is certain.
	if n, err := db.ReconcileInterruptedUpgrades(ctx, pool); err != nil {
		log.Printf("warning: %v", err)
	} else if n > 0 {
		log.Printf("MatrixCtrl: closed %d upgrade(s) left in flight by an earlier restart", n)
	}

	auditStore := audit.New(pool)

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
	// If no OIDC env is set, fall back to DB-persisted config (set by the
	// connect-OIDC setup flow). Env always wins.
	if oidcCfg.ClientID == "" {
		if dbCfg, ok := auth.LoadOIDCConfig(ctx, pool); ok {
			oidcCfg = dbCfg
			log.Printf("OIDC config loaded from DB (client_id=%s)", dbCfg.ClientID)
		}
	}
	var oidcSvc *auth.OIDCService
	// oidcRetry is non-nil only when the first attempt failed and a recovery loop is
	// worth starting. Wired to the auth handler further down so the login page can
	// tell "local login by design" from "Matrix login, issuer temporarily down".
	var oidcRetry *auth.RetryState
	if oidcCfg.ClientID != "" {
		// Non-fatal: if MAS isn't reachable yet (e.g. just deployed), start in
		// bootstrap mode — but keep trying, and hot-reload via the setup flow too.
		svc, err := auth.NewOIDCService(oidcCfg, pool, bootstrapAuth.JWTKey())
		if err != nil {
			log.Printf("WARNING: OIDC init failed (%v) — bootstrap mode for now, retrying in the background", err)
			oidcRetry = auth.NewRetryState()
		} else {
			oidcSvc = svc
			log.Printf("OIDC enabled: issuer=%s client_id=%s", oidcCfg.Issuer, oidcCfg.ClientID)
		}
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
	if helmClient != nil {
		// Persist the immutable per-revision facts the history page decodes, so the
		// cold read costs once per revision rather than once per pod. Without this
		// the in-memory cache is reset by every deploy, and the operator measured
		// 7.7 s on a page E39 had made 25 ms warm (etappe 42).
		helmClient.SetRevisionStore(db.NewRevisionFacts(pool))
	}

	// Auto-discover the ESS release if the configured one isn't found — lets
	// MatrixCtrl adopt an existing ESS without hard-coded namespace/release.
	if helmClient != nil {
		if _, err := helmClient.GetRelease(essRelease); err != nil {
			found, derr := helm.Discover(essNS)
			switch {
			case derr != nil:
			case len(found.Releases) == 1:
				essNS, essRelease = found.Releases[0].Namespace, found.Releases[0].Name
				log.Printf("ESS auto-discovered: release=%s namespace=%s version=%s", essRelease, essNS, found.Releases[0].Version)
				if c, cerr := helm.New(essNS); cerr == nil {
					helmClient = c
				}
			case len(found.Releases) > 1:
				log.Printf("note: %d ESS releases found — set MATRIXCTRL_ESS_RELEASE/NAMESPACE to choose", len(found.Releases))
			case !found.ClusterWide:
				// Nothing found, and the search never left this namespace. Saying so
				// keeps the log from implying the cluster was checked and is empty.
				log.Printf("note: no ESS release in namespace %s; a cluster-wide search needs read access to every Secret, which the namespaced role does not grant", found.Namespace)
			}
		}
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

	authHandler := handlers.NewAuthHandler(bootstrapAuth, oidcSvc, pool, bootstrapAuth.JWTKey())
	statusHandler := handlers.NewStatusHandler(k8sClient, helmClient, essNS, essRelease, frontendFS)
	hooksHandler := handlers.NewHooksHandler(pool, engine)
	driftHandler := handlers.NewDriftHandler(pool, k8sClient, essNS)
	usersHandler := handlers.NewUsersHandler(authHandler.MAS)
	rtcHandler := handlers.NewRTCHandler(k8sClient, configStore, essNS, essRelease, pool)
	helmHandler := handlers.NewHelmHandler(helmClient, pool, engine, essRelease, configStore, k8sClient, essNS)
	helmHandler.SetOIDCReloader(authHandler.ReloadOIDC)

	// Rooms (E36). Synapse is reached in-cluster rather than through the public
	// hostname: the admin API would otherwise leave the cluster, cross the ingress and
	// the tunnel, and come back — three more places for a bearer token to be logged,
	// for a call between two pods in the same namespace.
	synapseURL := env("MATRIXCTRL_SYNAPSE_URL",
		fmt.Sprintf("http://%s-synapse-main.%s.svc.cluster.local:8008", essRelease, essNS))
	matrixTokens := auth.NewMatrixTokens(func(ctx context.Context, refreshToken string) (string, string, int, error) {
		// Resolved per call rather than captured: connect-OIDC and the E33 retry can
		// both replace the OIDC service at runtime, and a captured one would keep
		// refreshing against the configuration that has since been replaced.
		o := authHandler.OIDC()
		if o == nil {
			return "", "", 0, fmt.Errorf("OIDC is not configured")
		}
		return o.RefreshSynapseAdmin(ctx, refreshToken)
	})
	authHandler.SetMatrixTokens(matrixTokens)

	// One factory for both screens: the token belongs to the caller, so a client
	// captured at construction would carry one operator's authority into another
	// operator's request (etappe 36).
	synapseFor := func(userID string) *synapse.Client {
		return synapse.New(synapseURL, func(ctx context.Context) (string, error) {
			return matrixTokens.Get(ctx, userID)
		})
	}

	// One disposition store per queue. Synapse numbers event reports and user reports
	// independently, so the kind is part of a report's identity (etappe 48).
	reportsHandler := handlers.NewReportsHandler(synapseFor,
		synapse.NewDispositions(pool, synapse.KindEvent),
		synapse.NewDispositions(pool, synapse.KindUser))

	roomsHandler := handlers.NewRoomsHandler(
		func(userID string) *synapse.Client {
			return synapse.New(synapseURL, func(ctx context.Context) (string, error) {
				return matrixTokens.Get(ctx, userID)
			})
		},
		matrixTokens.Has,
		func(ctx context.Context) (string, error) {
			o := authHandler.OIDC()
			if o == nil {
				return "", fmt.Errorf("OIDC is not configured")
			}
			return o.SynapseAdminAuthURL(ctx)
		},
	)

	// Recover from a failed OIDC start without a restart (E33).
	//
	// It rebuilds from `oidcCfg` — the *effective* startup config — rather than
	// calling ReloadOIDC. ReloadOIDC reads the DB only, and env wins over the DB at
	// startup, so on an env-configured instance (this one) it would find nothing,
	// return success, and leave OIDC off forever while the logs claimed a recovery
	// was under way. A silent no-op would be worse than the bug it replaces.
	if oidcRetry != nil {
		authHandler.SetRetryState(oidcRetry)
		go auth.RetryOIDC(ctx, oidcRetry, auth.RetryTarget{
			Build: func(context.Context) (*auth.OIDCService, error) {
				return auth.NewOIDCService(oidcCfg, pool, bootstrapAuth.JWTKey())
			},
			Installed: authHandler.OIDCConfigured, // the setup flow may have won
			Install:   authHandler.InstallOIDC,
			Wait:      auth.SleepOrDone,
		})
	}
	wsHandler := handlers.NewWSHandler(helmHandler)
	configHandler := handlers.NewConfigHandler(configStore, configGit, essVersion)
	setupHandler := handlers.NewSetupHandler(helmClient, configStore, essRelease, essNS, oidcSvc != nil && oidcSvc.Enabled())

	router := api.NewRouter(api.Deps{
		Auth:    authHandler,
		Status:  statusHandler,
		Hooks:   hooksHandler,
		Drift:   driftHandler,
		Helm:    helmHandler,
		WS:      wsHandler,
		Config:  configHandler,
		Setup:   setupHandler,
		Audit:   handlers.NewAuditHandler(auditStore),
		RTC:     rtcHandler,
		Users:   usersHandler,
		Rooms:   roomsHandler,
		Reports: reportsHandler,

		AuditSink: auditStore,
	})

	// Observe the announced RTC address on a timer. Doing it only on page view
	// would leave the history with gaps exactly where nobody was looking, and the
	// thing being measured is *when* the address changed.
	rtcStore := rtc.NewStore(pool)
	go rtc.NewWatcher(rtcStore, rtcHandler.AnnouncedHost, 0).Start(context.Background())

	// Sample the SFU's counters on a timer, for a stronger version of the same
	// reason: LiveKit's counters are process-lifetime, and the post-upgrade hook
	// deletes the SFU pod on every ESS upgrade. Unrecorded, the call history does
	// not merely have gaps — it is destroyed several times a week (E44).
	go rtc.NewSampler(rtcStore, rtcHandler.MetricsReader(), 0).Start(context.Background())

	// Both of the above append forever. On a single-node cluster sharing a disk with
	// Synapse's database and media, that is a real trap — and the sampler above adds
	// 1440 rows a day (E45).
	go rtcStore.StartPruning(context.Background())

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
