// First-time setup: deploying ESS onto an empty cluster and connecting Matrix
// login. This is the code path our own instance can never reach — ESS exists here,
// so the guards short-circuit — which is why etappe 15 had to run it on a throwaway
// cluster to discover it had never worked.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/auth"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
)

// DeployESS performs a greenfield ESS install (Phase 1.5): seed the config from
// the chart's commented defaults, apply server name + derived hostnames, then
// helm install. Refuses if a release already exists. Streams progress like Upgrade.
func (h *HelmHandler) DeployESS(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	var req struct {
		Version    string `json:"version"`
		ServerName string `json:"server_name"`
	}
	if err := Decode(r, &req); err != nil || req.Version == "" || req.ServerName == "" {
		Error(w, http.StatusBadRequest, "version and server_name are required")
		return
	}

	// Guard: never clobber an existing release.
	if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
		Error(w, http.StatusConflict, "release '"+h.essRelease+"' already exists — use Upgrade, not Deploy")
		return
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	sn := req.ServerName
	version := req.Version

	go func() {
		ctx := context.Background()

		stream.emit("Pulling ESS chart " + version + " for default config…")
		values, err := h.helm.DefaultChartValues(version)
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			return
		}

		// Seed only when the repo is empty, and treat "already populated" as a
		// state to continue from rather than an error.
		//
		// Deploy only reaches this point when no ESS release exists (checked
		// above), so config without a release means an earlier attempt got part
		// of the way and stopped. Failing here made that unrecoverable: the
		// wizard refused to run again and the operator was stuck with no way
		// forward from the UI. Since every greenfield deploy failed before etappe
		// 15, that is the state everyone would have been left in.
		//
		// Skipping rather than force-overwriting is deliberate — the config may
		// have been prepared on purpose, and destroying it to retry a deploy
		// would be a worse failure than the one being fixed.
		existing, _ := h.configStore.List(ctx)
		if len(existing) > 0 {
			stream.emit(fmt.Sprintf(
				"Config repo already has %d sections — keeping them and continuing.", len(existing)))
		} else {
			stream.emit("Seeding per-section config from chart defaults…")
			if err := h.configStore.SeedSections(ctx, values, false); err != nil {
				stream.emit("ERROR: seed config: " + err.Error())
				stream.finish("failed")
				return
			}
		}

		changes := greenfieldHostnames(sn)
		if err := h.configStore.SetSectionValues(ctx, changes, greenfieldRemovals()); err != nil {
			stream.emit("WARNING: could not apply hostnames: " + err.Error())
		}
		if _, err := h.configStore.Commit(ctx, "config: greenfield seed for "+sn, userID); err != nil {
			stream.emit("WARNING: git commit: " + err.Error())
		}
		stream.emit("Server name set to " + sn + " with derived hostnames.")

		contents, _ := h.configStore.MergedContent(ctx)
		merged, err := config.MergeToMap(contents)
		if err != nil {
			stream.emit("ERROR: merge config: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit("Installing ESS " + version + " — this can take several minutes…")
		stopProgress := stream.startProgress("Waiting for install", upgradeProgressInterval)
		result, err := h.helm.Install(ctx, h.essRelease, version, merged)
		stopProgress()
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit(fmt.Sprintf("ESS installed (revision %s). Running post-install hooks…", intToStr(result.Revision)))
		_, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, "deploy:"+h.essRelease, userID)
		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
			stream.emit("WARNING: post-install hooks failed: " + hookErr.Error())
		} else {
			stream.emit("ESS deployed successfully. Configure Matrix login under Setup once MAS is up.")
		}
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{"upgrade_id": upgradeID})
}

// ConnectOIDC registers MatrixCtrl's own OIDC client in MAS — via the config it
// already manages (writes the client + admin_clients into the
// matrixAuthenticationService section, then helm-upgrades ESS so MAS picks it up),
// stores the OIDC settings in the DB, and hot-reloads auth into OIDC mode. This
// closes the bootstrap→OIDC loop without manual MAS patching or a restart.
func (h *HelmHandler) ConnectOIDC(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	var req struct {
		Issuer    string `json:"issuer"`     // MAS public URL, e.g. https://mas-matrix.example.com
		PublicURL string `json:"public_url"` // MatrixCtrl public base, e.g. https://matrixctrl.example.com
	}
	if err := Decode(r, &req); err != nil || req.Issuer == "" || req.PublicURL == "" {
		Error(w, http.StatusBadRequest, "issuer and public_url are required")
		return
	}

	// Idempotency guard: refuse if a MatrixCtrl client is already registered.
	if contents, err := h.configStore.MergedContent(r.Context()); err == nil {
		if merged, err := config.MergeToMap(contents); err == nil {
			if nestedGet(merged, "matrixAuthenticationService", "additional", "0-matrixctrl-client") != nil {
				Error(w, http.StatusConflict, "a MatrixCtrl OIDC client is already registered in MAS config")
				return
			}
		}
	}

	clientID := auth.GenerateULID()
	secret := auth.GenerateSecret()
	issuer := strings.TrimRight(req.Issuer, "/")
	redirect := strings.TrimRight(req.PublicURL, "/") + "/api/v1/auth/oidc/callback"
	fragment := buildMASClientConfig(clientID, secret, redirect)

	// Write the client into the matrixAuthenticationService section (comment-preserving).
	changes := map[string]interface{}{
		"matrixAuthenticationService.additional.0-matrixctrl-client.config": fragment,
	}
	if err := h.configStore.SetSectionValues(r.Context(), changes, nil); err != nil {
		Error(w, http.StatusInternalServerError, "write MAS client config: "+err.Error())
		return
	}
	if _, err := h.configStore.Commit(r.Context(), "config: register MatrixCtrl OIDC client in MAS", userID); err != nil {
		// non-fatal
		_ = err
	}
	// Persist OIDC settings so MatrixCtrl can use them after reload.
	if err := auth.SaveOIDCConfig(r.Context(), h.db, auth.OIDCConfig{
		Issuer: issuer, ClientID: clientID, ClientSecret: secret, RedirectURI: redirect,
	}); err != nil {
		Error(w, http.StatusInternalServerError, "save oidc config: "+err.Error())
		return
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	go func() {
		ctx := context.Background()
		stream.emit("MatrixCtrl client written into MAS config (client_id=" + clientID + ").")

		rel, err := h.helm.GetRelease(h.essRelease)
		if err != nil || rel == nil {
			stream.emit("ERROR: ESS release not found — deploy ESS first.")
			stream.finish("failed")
			return
		}
		contents, _ := h.configStore.MergedContent(ctx)
		merged, _ := config.MergeToMap(contents)

		stream.emit("Upgrading ESS so MAS loads the new client (this restarts MAS)…")
		stopProgress := stream.startProgress("Waiting for Helm rollout", upgradeProgressInterval)
		_, upgradeErr := h.helm.Upgrade(ctx, h.essRelease, rel.Version, merged)
		stopProgress()
		if err := upgradeErr; err != nil {
			stream.emit("ERROR: helm upgrade: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit("Waiting for MAS to come back up with the client…")
		var reloadErr error
		for i := 0; i < 12; i++ {
			time.Sleep(5 * time.Second)
			if h.oidcReloader == nil {
				break
			}
			if reloadErr = h.oidcReloader(ctx); reloadErr == nil {
				break
			}
			stream.emit("  …MAS not ready yet, retrying")
		}
		if reloadErr != nil {
			stream.emit("WARNING: client registered but OIDC reload failed: " + reloadErr.Error())
			stream.emit("Reload manually from Setup once MAS is ready.")
			stream.finish("hooks-failed")
			return
		}

		stream.emit("Matrix login connected. Log out and back in via Matrix.")
		stream.finish("success")
	}()

	JSON(w, http.StatusAccepted, map[string]string{"upgrade_id": upgradeID, "client_id": clientID})
}

// buildMASClientConfig renders the inner MAS config fragment (a string the ESS
// chart embeds verbatim) registering a static client + granting it admin.
//
// client_name is what MAS shows on the consent screen. Without it the operator is
// asked to "Continue to 01KSPV9ZMR7NB4B2BBWMPYSD1P?" — a ULID, which looks like
// something is wrong rather than like their own admin tool.
//
// It is not in MAS's documented field list, and MAS ignores unknown keys in this
// section (verified against 1.15.0: a deliberately bogus field also passes
// `mas-cli config check`). So this cannot break MAS startup — but whether it
// actually renders depends on the MAS version. Upstream issue #4415 reports the
// field existing for static clients yet not being synced to the database; it is
// closed, so recent versions should honour it. Harmless if they do not.
func buildMASClientConfig(clientID, secret, redirect string) string {
	return fmt.Sprintf(`clients:
  - client_id: "%s"
    client_name: "MatrixCtrl"
    client_auth_method: client_secret_basic
    client_secret: "%s"
    redirect_uris:
      - "%s"
policy:
  data:
    admin_clients:
      - "%s"
`, clientID, secret, redirect, clientID)
}

// greenfieldHostnames maps a server name to the values the deploy wizard seeds.
//
// Every key here must exist in matrix-stack's values.schema.json, which sets
// additionalProperties:false — an unknown key does not degrade gracefully, it
// makes `helm install` fail validation and takes the whole greenfield deploy with
// it.
//
// That is not hypothetical. `wellKnownDelegation.ingress.host` used to be in this
// map, and its ingress schema has no `host` property: well-known is served at the
// server name itself, which the chart derives from serverName. So every greenfield
// deploy failed with "Additional property host is not allowed" — the product's
// headline claim, broken from the first day, because our own instance already has
// ESS and can never reach this code path. Etappe 15 ran it on an empty cluster for
// the first time and it failed immediately.
//
// Before adding a component here, check its ingress block in
// matrix-stack/values.schema.json actually accepts `host`.
func greenfieldHostnames(serverName string) map[string]interface{} {
	return map[string]interface{}{
		"serverName":           serverName,
		"synapse.ingress.host": "matrix." + serverName,
		"matrixAuthenticationService.ingress.host": "mas." + serverName,
		"elementWeb.ingress.host":                  "element." + serverName,
		"elementAdmin.ingress.host":                "admin." + serverName,
		"matrixRTC.ingress.host":                   "mrtc." + serverName,
	}
}

// greenfieldRemovals lists config keys that must not survive into a deploy,
// because matrix-stack's schema rejects them and `helm install` fails on the
// whole release.
//
// This heals repos written by an older build. Removing the key from
// greenfieldHostnames stops it being written again, but an operator who already
// tried the broken deploy has it sitting in their config repo — and since the
// wizard now keeps existing config rather than overwriting it, the bad value
// would survive every retry. The people most in need of the fix would have been
// the only ones it did not reach.
func greenfieldRemovals() []string {
	return []string{
		// Its ingress block has no `host` property and sets
		// additionalProperties:false — well-known is served at the server name
		// itself, which the chart derives from serverName.
		"wellKnownDelegation.ingress.host",
	}
}
