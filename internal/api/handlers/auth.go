package handlers

import (
	"context"
	"net"
	"net/http"

	authmw "github.com/bxnny/matrixctrl/internal/api/middleware"
)

type TokenService interface {
	Login(ctx context.Context, username, password, ip, ua string) (string, error)
	ValidateToken(token string) (string, error)
	RevokeSession(ctx context.Context, token string) error
}

type AuthHandler struct {
	svc TokenService
}

func NewAuthHandler(svc TokenService) *AuthHandler {
	return &AuthHandler{svc: svc}
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
	token, err := h.svc.Login(r.Context(), req.Username, req.Password,
		ip, r.UserAgent())
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

func (h *AuthHandler) OIDCRedirect(w http.ResponseWriter, r *http.Request) {
	Error(w, http.StatusNotImplemented, "OIDC not yet configured (Phase 1)")
}

func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	Error(w, http.StatusNotImplemented, "OIDC not yet configured (Phase 1)")
}
