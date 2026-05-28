package handlers

import (
	"context"
	"net"
	"net/http"
	"net/url"

	authmw "github.com/bxnny/matrixctrl/internal/api/middleware"
	"github.com/bxnny/matrixctrl/internal/auth"
)

type TokenService interface {
	Login(ctx context.Context, username, password, ip, ua string) (string, error)
	ValidateToken(token string) (string, error)
	RevokeSession(ctx context.Context, token string) error
}

type AuthHandler struct {
	svc  TokenService
	oidc *auth.OIDCService
}

func NewAuthHandler(svc TokenService, oidcSvc *auth.OIDCService) *AuthHandler {
	return &AuthHandler{svc: svc, oidc: oidcSvc}
}

func (h *AuthHandler) ValidateToken(token string) (string, error) {
	return h.svc.ValidateToken(token)
}

func (h *AuthHandler) BootstrapLogin(w http.ResponseWriter, r *http.Request) {
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
	token, err := h.svc.Login(r.Context(), req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 {
		token = token[7:]
	}
	_ = h.svc.RevokeSession(r.Context(), token)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	JSON(w, http.StatusOK, map[string]string{"user_id": userID})
}

// GET /api/v1/auth/oidc/available — lets the frontend know if OIDC is configured.
func (h *AuthHandler) OIDCAvailable(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]bool{"enabled": h.oidc != nil && h.oidc.Enabled()})
}

// GET /api/v1/auth/oidc/redirect — generates a state and redirects the browser to MAS.
func (h *AuthHandler) OIDCRedirect(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil || !h.oidc.Enabled() {
		Error(w, http.StatusNotImplemented, "OIDC not configured — set MATRIXCTRL_OIDC_* env vars")
		return
	}
	authURL, err := h.oidc.AuthURL(r.Context())
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

	userID, err := h.oidc.ExchangeCode(r.Context(), code, state)
	if err != nil {
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	token, err := h.oidc.CreateOIDCSession(r.Context(), userID, ip, r.UserAgent())
	if err != nil {
		http.Redirect(w, r, "/auth/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	// Hand the token to the SPA via query param; the /auth/callback route
	// reads it, stashes it in localStorage, and redirects to /.
	http.Redirect(w, r, "/auth/callback?token="+url.QueryEscape(token), http.StatusFound)
}
