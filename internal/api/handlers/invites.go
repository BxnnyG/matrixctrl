package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/mas"
)

// InvitesHandler serves invitation codes: MAS registration tokens (etappe 125).
//
// The code itself is the secret here, so it travels only in the response and in the
// request body. The audit middleware records neither; the paths carry the MAS id, not
// the code, so the trail says "revoked invite 01ABC" without ever holding a usable code.
type InvitesHandler struct {
	// A function for the same reason as UsersHandler: the MAS client appears at
	// runtime when the connect-OIDC flow completes.
	client func() *mas.Client
	// MayWrite decides who sees the codes themselves: a code is an access, so a session
	// that may only look gets the list without it. Nil means everybody (no role set).
	MayWrite func(userID string) bool
	// MASHost returns the public MAS host ("" if unknown); nil means unknown.
	MASHost func(ctx context.Context) string
}

func NewInvitesHandler(client func() *mas.Client) *InvitesHandler {
	return &InvitesHandler{client: client}
}

const noMAS = "Einladungen brauchen MAS-Zugang. MatrixCtrl ist (noch) nicht mit MAS verbunden."

// inviteJSON is the wire shape. Field names are the frontend's contract.
func inviteJSON(t mas.RegistrationToken) map[string]any {
	return map[string]any{
		"id":           t.ID,
		"token":        t.Token,
		"token_hidden": false,
		"valid":        t.Valid,
		"state":        t.State(), // valid | used | expired | revoked
		"usage_limit":  t.UsageLimit,
		"times_used":   t.TimesUsed,
		"created_at":   t.CreatedAt,
		"last_used_at": t.LastUsedAt,
		"expires_at":   t.ExpiresAt,
		"revoked_at":   t.RevokedAt,
	}
}

// List answers GET /api/v1/invites[?filter=valid|used|expired|revoked].
func (h *InvitesHandler) List(w http.ResponseWriter, r *http.Request) {
	client := h.client()
	if !client.Configured() {
		// 200 like the user list: not a failure the operator can retry away, and a 503
		// on a page load is a red console line in every browser (make e2e caught it).
		// Writes still answer 503.
		JSON(w, http.StatusOK, map[string]any{"configured": false, "invites": []any{}, "register_url": ""})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	list, err := client.ListRegistrationTokens(ctx, r.URL.Query().Get("filter"))
	if err != nil {
		h.fail(w, err)
		return
	}
	hide := h.MayWrite != nil && !h.MayWrite(authmw.UserIDFromContext(r.Context()))
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		j := inviteJSON(t)
		if hide {
			j["token"], j["token_hidden"] = "", true
		}
		out = append(out, j)
	}
	JSON(w, http.StatusOK, map[string]any{"configured": true, "invites": out, "register_url": h.registerURL(ctx)})
}

type inviteBody struct {
	Token      string  `json:"token"`
	UsageLimit *int    `json:"usage_limit"`
	ExpiresAt  *string `json:"expires_at"`
}

func readInviteBody(r *http.Request) (inviteBody, *time.Time, bool) {
	var b inviteBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&b); err != nil {
		return b, nil, false
	}
	var exp *time.Time
	if b.ExpiresAt != nil && *b.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *b.ExpiresAt)
		if err != nil {
			return b, nil, false
		}
		exp = &t
	}
	return b, exp, true
}

// Create answers POST /api/v1/invites {token?, usage_limit?, expires_at?}.
func (h *InvitesHandler) Create(w http.ResponseWriter, r *http.Request) {
	client := h.client()
	if !client.Configured() {
		Error(w, http.StatusServiceUnavailable, noMAS)
		return
	}
	b, exp, ok := readInviteBody(r)
	if !ok {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t, err := client.CreateRegistrationToken(ctx, b.Token, b.UsageLimit, exp)
	if err != nil {
		h.fail(w, err)
		return
	}
	JSON(w, http.StatusOK, inviteJSON(*t))
}

// Update answers PUT /api/v1/invites/{id} {usage_limit, expires_at}; null or missing
// clears the field.
func (h *InvitesHandler) Update(w http.ResponseWriter, r *http.Request) {
	client := h.client()
	if !client.Configured() {
		Error(w, http.StatusServiceUnavailable, noMAS)
		return
	}
	b, exp, ok := readInviteBody(r)
	if !ok {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t, err := client.EditRegistrationToken(ctx, chi.URLParam(r, "id"), b.UsageLimit, exp)
	if err != nil {
		h.fail(w, err)
		return
	}
	JSON(w, http.StatusOK, inviteJSON(*t))
}

// Revoke answers POST /api/v1/invites/{id}/revoke and /unrevoke. The action comes from
// the route, as with users, because the audit trail keeps only the path.
func (h *InvitesHandler) Revoke(undo bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := h.client()
		if !client.Configured() {
			Error(w, http.StatusServiceUnavailable, noMAS)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var t *mas.RegistrationToken
		var err error
		if undo {
			t, err = client.UnrevokeRegistrationToken(ctx, chi.URLParam(r, "id"))
		} else {
			t, err = client.RevokeRegistrationToken(ctx, chi.URLParam(r, "id"))
		}
		if err != nil {
			h.fail(w, err)
			return
		}
		JSON(w, http.StatusOK, inviteJSON(*t))
	}
}

func (h *InvitesHandler) fail(w http.ResponseWriter, err error) {
	var ae *mas.ActionError
	if errors.As(err, &ae) {
		status := http.StatusBadGateway
		switch ae.Status {
		case http.StatusNotFound:
			status = http.StatusNotFound
		case http.StatusBadRequest:
			status = http.StatusBadRequest
		case http.StatusConflict:
			status = http.StatusConflict
		case http.StatusForbidden:
			status = http.StatusForbidden
		}
		Error(w, status, ae.Msg)
		return
	}
	Error(w, http.StatusBadGateway, "MAS antwortet nicht: "+err.Error())
}

// registerURL is where an invited person registers; empty when the host is unknown.
func (h *InvitesHandler) registerURL(ctx context.Context) string {
	if h.MASHost == nil {
		return ""
	}
	if host := h.MASHost(ctx); host != "" {
		return "https://" + host + "/register"
	}
	return ""
}
