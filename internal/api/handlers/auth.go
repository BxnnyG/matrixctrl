package handlers

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/auth"
	"github.com/bxnnyg/matrixctrl/internal/mas"
)

type TokenService interface {
	Login(ctx context.Context, username, password, ip, ua string) (string, error)
	ValidateToken(token string) (string, error)
	RevokeSession(ctx context.Context, token string) error
}

type AuthHandler struct {
	svc    TokenService
	db     *pgxpool.Pool
	jwtKey []byte
	// codes issues and redeems the one-time login codes that replaced putting the
	// session JWT in a URL (P0-5).
	codes *auth.AuthCodes
	// throttle limits failed password attempts, counted in Postgres so a restart
	// does not reset them (P1-17).
	throttle *auth.Throttle
	// tickets are the single-use credentials the WebSocket handshake uses instead of
	// the session token, which used to be logged verbatim (E35).
	tickets *auth.WSTickets
	// matrixTokens holds each operator's Synapse-admin session, in memory only, for
	// rooms and moderation. Nil when the feature is not wired up (E36).
	matrixTokens *auth.MatrixTokens

	mu   sync.RWMutex
	oidc *auth.OIDCService
	// retry is the state of the background OIDC recovery loop, or nil when OIDC was
	// never configured. Read by the login page so a password box that appears because
	// the IdP is down does not look like the normal way in (E33).
	retry *auth.RetryState
}

func NewAuthHandler(svc TokenService, oidcSvc *auth.OIDCService, db *pgxpool.Pool, jwtKey []byte) *AuthHandler {
	return &AuthHandler{
		svc: svc, oidc: oidcSvc, db: db, jwtKey: jwtKey,
		codes:    auth.NewAuthCodes(db),
		throttle: auth.NewThrottle(db),
		tickets:  auth.NewWSTickets(),
	}
}

func (h *AuthHandler) getOIDC() *auth.OIDCService {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.oidc
}

func (h *AuthHandler) OIDCConfigured() bool {
	o := h.getOIDC()
	return o != nil && o.Enabled()
}

// InstallOIDC publishes a service built elsewhere — today, by the background retry
// after a failed start (E33). It writes under the same lock ReloadOIDC uses, so a
// swap mid-request hands back a consistent service rather than a torn read.
//
// Bootstrap login closes the moment this lands: BootstrapLogin refuses to run while
// OIDC is configured, and it reads through the same lock.
func (h *AuthHandler) InstallOIDC(svc *auth.OIDCService) {
	h.mu.Lock()
	h.oidc = svc
	h.mu.Unlock()
}

// SetRetryState hands the handler the loop's observable state. Set once at startup,
// before the server serves.
func (h *AuthHandler) SetRetryState(s *auth.RetryState) { h.retry = s }

// SetMatrixTokens wires the in-memory store of operators' Synapse-admin sessions.
func (h *AuthHandler) SetMatrixTokens(m *auth.MatrixTokens) { h.matrixTokens = m }

// POST /api/v1/auth/ws-ticket — a single-use ticket for one WebSocket handshake.
//
// A browser cannot set an Authorization header on a WebSocket, so something has to
// travel in the URL, and URLs are logged — by us, by the ingress, by the tunnel. This
// hands out something that is worth nothing after one use, so a ticket in any of
// those logs opens nothing (E35).
func (h *AuthHandler) WSTicket(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	if userID == "" {
		// Should be unreachable behind the auth middleware. If it is ever reached,
		// issuing a ticket would be issuing one to nobody.
		Error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	ticket, err := h.tickets.Issue(userID)
	if err != nil {
		Error(w, http.StatusServiceUnavailable, "could not issue a ticket")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

// RedeemWSTicket spends a ticket. Wired into the auth middleware so the WebSocket
// handshake authenticates without carrying the session token.
func (h *AuthHandler) RedeemWSTicket(ticket string) (string, bool) {
	return h.tickets.Redeem(ticket)
}

// ReloadOIDC rebuilds the OIDC service from the DB-persisted config and swaps it
// in atomically. Used by the connect-OIDC flow to switch from bootstrap to OIDC at
// runtime (no restart). Safe to call repeatedly.
func (h *AuthHandler) ReloadOIDC(ctx context.Context) error {
	cfg, ok := auth.LoadOIDCConfig(ctx, h.db)
	if !ok {
		return nil // nothing persisted yet
	}
	svc, err := auth.NewOIDCService(cfg, h.db, h.jwtKey)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.oidc = svc
	h.mu.Unlock()
	log.Printf("OIDC hot-reloaded: issuer=%s client_id=%s", cfg.Issuer, cfg.ClientID)
	return nil
}

// MAS returns the current OIDC service's shared MAS admin client, or nil in
// bootstrap mode. Read through the same lock ReloadOIDC writes under, so a swap
// mid-request hands back a consistent client rather than a torn read.
func (h *AuthHandler) MAS() *mas.Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.oidc == nil {
		return nil
	}
	return h.oidc.MAS()
}

func (h *AuthHandler) ValidateToken(token string) (string, error) {
	return h.svc.ValidateToken(token)
}

func (h *AuthHandler) BootstrapLogin(w http.ResponseWriter, r *http.Request) {
	// Once OIDC is active, the local bootstrap login is disabled (public-facing).
	if h.OIDCConfigured() {
		Error(w, http.StatusForbidden, "bootstrap login disabled — use Matrix login")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}

	// Both keys are checked: per-IP stops one source hammering every account, and
	// per-user stops a distributed attempt on one account. Either being locked
	// refuses the request (P1-17).
	keys := []string{"ip:" + ip, "user:" + strings.ToLower(req.Username)}
	var delay time.Duration
	for _, key := range keys {
		d, err := h.throttle.Check(r.Context(), key)
		if err != nil {
			var locked *auth.ErrLockedOut
			if errors.As(err, &locked) {
				Error(w, http.StatusTooManyRequests, locked.Error())
				return
			}
			// The limiter could not be consulted. Refusing is the uncomfortable
			// direction and the right one: the alternative is that taking the
			// database down disables the rate limit. The login needs Postgres to
			// verify a password anyway, so nothing that worked is lost.
			log.Printf("login throttle unavailable: %v", err)
			Error(w, http.StatusServiceUnavailable, "Anmeldung derzeit nicht möglich")
			return
		}
		if d > delay {
			delay = d
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}

	token, err := h.svc.Login(r.Context(), req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		for _, key := range keys {
			if ferr := h.throttle.Failed(r.Context(), key); ferr != nil {
				log.Printf("could not record failed login: %v", ferr)
			}
		}
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Only a correct password clears the counter; anything else would hand an
	// attacker a way to reset their own budget.
	for _, key := range keys {
		if cerr := h.throttle.Succeeded(r.Context(), key); cerr != nil {
			log.Printf("could not clear login attempts: %v", cerr)
		}
	}

	JSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 {
		token = token[7:]
	}
	_ = h.svc.RevokeSession(r.Context(), token)

	// Signing out drops the operator's Matrix session too. Leaving it behind would
	// mean a refresh token that outlived the login it came with, still able to mint
	// Synapse-admin tokens for someone who has left (E36).
	if h.matrixTokens != nil {
		h.matrixTokens.Forget(authmw.UserIDFromContext(r.Context()))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	JSON(w, http.StatusOK, map[string]string{"user_id": userID})
}

// GET /api/v1/auth/oidc/available — lets the frontend know if OIDC is configured.
// GET /api/v1/auth/oidc/available — unauthenticated; the login page calls it before
// anyone has a session.
//
// `retrying` distinguishes "this install uses local login" from "Matrix login exists
// but its issuer is unreachable right now". Those look identical on screen and lead to
// opposite actions: wait, versus go find your password. It carries no error detail —
// the endpoint is public, and the reason is in the log, which already needs access.
func (h *AuthHandler) OIDCAvailable(w http.ResponseWriter, r *http.Request) {
	o := h.getOIDC()
	enabled := o != nil && o.Enabled()
	JSON(w, http.StatusOK, map[string]bool{
		"enabled":  enabled,
		"retrying": !enabled && h.retry.Active(),
	})
}

// GET /api/v1/auth/oidc/redirect — generates a state and redirects the browser to MAS.
func (h *AuthHandler) OIDCRedirect(w http.ResponseWriter, r *http.Request) {
	o := h.getOIDC()
	if o == nil || !o.Enabled() {
		Error(w, http.StatusNotImplemented, "OIDC not configured")
		return
	}
	authURL, err := o.AuthURL(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// GET /api/v1/auth/oidc/callback — called by MAS after user authenticates.
func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	// MAS may report an error (e.g. user denied)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/auth/login?error=missing+code+or+state", http.StatusFound)
		return
	}

	o := h.getOIDC()
	if o == nil {
		http.Redirect(w, r, "/auth/login?error=OIDC+not+configured", http.StatusFound)
		return
	}
	// Both authorizations come back here, because MAS validates redirect_uris strictly
	// and the static client is registered with exactly one. Which flow this is comes
	// from the database row the state consumed, never from anything the browser
	// carried (E36).
	purpose, err := o.StatePurpose(r.Context(), state)
	if err != nil {
		log.Printf("OIDC callback error: %v", err)
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if purpose == auth.PurposeSynapseAdmin {
		h.finishSynapseAdmin(w, r, o, code)
		return
	}

	userID, err := o.LoginWithCode(r.Context(), code)
	if err != nil {
		log.Printf("OIDC callback error: %v", err)
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	// A one-time code, carried in the URL **fragment**, replaces handing the SPA
	// the session JWT in a query parameter. chi's request logger writes the full
	// URL, so the old form wrote the token to the application log by the very
	// request that delivered it (P0-5). Fragments are never sent to a server, so
	// there is nothing to log and nothing to leak through Referer; the code is
	// single-use and expires in a minute, so the copy in browser history is spent.
	code, cerr := h.codes.Issue(r.Context(), userID, ip, r.UserAgent())
	if cerr != nil {
		log.Printf("OIDC callback: could not issue login code: %v", cerr)
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape("Anmeldung konnte nicht abgeschlossen werden"), http.StatusFound)
		return
	}

	http.Redirect(w, r, "/auth/callback#code="+url.QueryEscape(code), http.StatusFound)
}

// ExchangeCode trades a one-time code for the session token, over a POST whose body
// is not logged. This is where the session is actually created — the code only
// proves that OIDC already said yes.
func (h *AuthHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	redeemed, err := h.codes.Redeem(r.Context(), req.Code)
	if err != nil {
		// One message for expired, unknown and already-used. Telling them apart
		// would say whether a code ever existed, which only helps an attacker.
		Error(w, http.StatusUnauthorized, "Anmeldecode ungültig oder abgelaufen")
		return
	}

	o := h.getOIDC()
	if o == nil {
		Error(w, http.StatusServiceUnavailable, "OIDC ist nicht konfiguriert")
		return
	}

	// The IP and user agent come from the redeemed code, not from this request:
	// they describe the browser that actually went through OIDC.
	token, err := o.CreateOIDCSession(r.Context(), redeemed.UserID, redeemed.IP, redeemed.UserAgent)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Sitzung konnte nicht erstellt werden")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"token": token})
}

// finishSynapseAdmin completes the separate authorization that grants the panel the
// operator's Synapse admin authority (E36).
//
// It resolves *which* operator from the token rather than from the session, because
// this callback is a public route with no auth middleware in front of it — the browser
// arriving here carries no MatrixCtrl credential. The identity is derived exactly as
// the login path derives it, so the Matrix session is filed under the key that later
// requests actually look up.
//
// The refresh token goes into memory and nowhere else. Nothing here logs any of it.
func (h *AuthHandler) finishSynapseAdmin(w http.ResponseWriter, r *http.Request, o *auth.OIDCService, code string) {
	fail := func(msg string) {
		http.Redirect(w, r, "/rooms?error="+url.QueryEscape(msg), http.StatusFound)
	}

	access, refresh, expiresIn, err := o.SynapseAdminTokens(r.Context(), code)
	if err != nil {
		// Deliberately not echoed to the browser: the error can carry an OAuth
		// description that names internals, and the operator can act on the short
		// version alone.
		log.Printf("synapse admin authorization failed: %v", err)
		fail("Der Matrix-Admin-Zugriff wurde nicht erteilt.")
		return
	}

	userID, err := o.UserID(r.Context(), access)
	if err != nil {
		log.Printf("synapse admin authorization: could not resolve the user: %v", err)
		fail("Der Zugriff konnte keinem Konto zugeordnet werden.")
		return
	}
	if h.matrixTokens == nil {
		fail("Räume sind auf dieser Installation nicht verfügbar.")
		return
	}

	h.matrixTokens.Put(userID, access, refresh, expiresIn)
	http.Redirect(w, r, "/rooms", http.StatusFound)
}

// OIDC returns the current OIDC service, or nil in bootstrap mode.
//
// Read through the same lock the swap writes under, so callers that resolve it per
// request — the rooms authorization, the token refresh — see whatever connect-OIDC or
// the E33 retry most recently installed rather than a copy captured at startup.
func (h *AuthHandler) OIDC() *auth.OIDCService {
	return h.getOIDC()
}
