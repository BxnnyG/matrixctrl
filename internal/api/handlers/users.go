package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	middleware "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/mas"
)

// UsersHandler serves the user list. Read-only for now: locking, deactivating,
// granting admin and resetting passwords are each destructive in a different way and
// need confirmation plus audit entries, which is its own etappe rather than a
// footnote to the one that introduces the subsystem.
type UsersHandler struct {
	// client is a function, not a value: the connect-OIDC flow rebuilds the OIDC
	// service at runtime (AuthHandler.ReloadOIDC), and a client captured at startup
	// would stay nil for the rest of the process on a greenfield install — the
	// exact path where someone configures OIDC and then wonders why users never
	// appear.
	client func() *mas.Client
}

func NewUsersHandler(client func() *mas.Client) *UsersHandler {
	return &UsersHandler{client: client}
}

// allowedStatuses is MAS's own vocabulary. Anything else is dropped rather than
// forwarded: an unknown value would come back as a 400 from MAS and surface as
// "something went wrong" for what is really a typo in a query string.
var allowedStatuses = map[string]bool{"active": true, "locked": true, "deactivated": true}

// List answers GET /api/v1/users.
//
// The source is MAS, which is authoritative for accounts under MSC3861. Synapse
// keeps its own user table and the two can disagree on a migrated stack, so the
// response says which one it read rather than implying it lists every user Synapse
// has ever seen.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	q := r.URL.Query()
	query := mas.UserQuery{
		Search:    q.Get("search"),
		After:     q.Get("after"),
		Before:    q.Get("before"),
		AdminOnly: q.Get("admin") == "true",
	}
	if s := q.Get("status"); allowedStatuses[s] {
		query.Status = s
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil {
		query.Limit = n
	}

	page, err := h.client().ListUsers(ctx, query)
	if err != nil {
		if errors.Is(err, mas.ErrNotConfigured) {
			// Not an error the operator can fix by retrying, and not a failure of
			// the cluster. Its own status so the page can explain instead of
			// showing a red box.
			JSON(w, http.StatusOK, map[string]any{
				"configured": false,
				"users":      []any{},
				"source":     "mas",
			})
			return
		}
		Error(w, http.StatusBadGateway, "MAS antwortet nicht: "+err.Error())
		return
	}

	users := make([]map[string]any, 0, len(page.Users))
	for _, u := range page.Users {
		users = append(users, map[string]any{
			"id":       u.ID,
			"username": u.Username,
			// Both timestamps travel, not just the collapsed word: locked is
			// reversible and usually temporary, deactivated is the account being
			// gone, and the operator needs to know which.
			"created_at":     u.CreatedAt,
			"locked_at":      u.LockedAt,
			"deactivated_at": u.DeactivatedAt,
			"state":          u.State(),
			"admin":          u.Admin,
			"legacy_guest":   u.LegacyGuest,
		})
	}

	JSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"users":      users,
		"total":      page.Total,
		"next":       page.Next,
		"prev":       page.Prev,
		"source":     "mas",
	})
}

// Write actions.
//
// The endpoints are verb-in-path — /grant-admin and /revoke-admin rather than one
// /set-admin taking a boolean — because the audit middleware deliberately records no
// request body. The path is therefore the only place a meaning can live, and an
// audit trail that says "set-admin" without saying which way is a trail that cannot
// answer the question it exists for.

// selfProtected are the actions that would lock the acting user out of the very
// panel they would need in order to undo them. MatrixCtrl only admits MAS admins,
// so deactivating yourself, locking yourself or revoking your own admin is a
// one-way door.
var selfProtected = map[string]string{
	"lock":         "Du würdest dich selbst sperren und könntest MatrixCtrl nicht mehr benutzen.",
	"deactivate":   "Du würdest dein eigenes Konto deaktivieren und dich damit aus MatrixCtrl aussperren.",
	"revoke-admin": "Du würdest dir selbst die Admin-Rechte entziehen — danach lässt MatrixCtrl dich nicht mehr herein.",
	"set-password": "", // allowed: changing your own password does not lock you out
}

// Act performs one write action. The action comes from the route, not from the body,
// so a request cannot ask for something the router did not expose.
func (h *UsersHandler) Act(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		client := h.client()
		if !client.Configured() {
			Error(w, http.StatusServiceUnavailable,
				"Benutzerverwaltung braucht MAS-Zugang. MatrixCtrl läuft im Bootstrap-Modus.")
			return
		}

		id := chi.URLParam(r, "id")
		if id == "" {
			Error(w, http.StatusBadRequest, "Keine Benutzer-ID angegeben.")
			return
		}

		if reason, guarded := selfProtected[action]; guarded && reason != "" {
			same, err := h.isActingUser(ctx, r, client, id)
			if err != nil {
				// Refusing is the safe direction. "I could not tell whether this is
				// you" is not permission — a safety rail that gives way when it is
				// unsure is one that is trusted and does not hold.
				Error(w, http.StatusServiceUnavailable,
					"Konnte nicht feststellen, ob das dein eigenes Konto ist — die Aktion wurde deshalb nicht ausgeführt.")
				return
			}
			if same {
				Error(w, http.StatusConflict, reason)
				return
			}
		}

		var err error
		switch action {
		case "lock":
			err = client.Lock(ctx, id)
		case "unlock":
			err = client.Unlock(ctx, id)
		case "deactivate":
			err = client.Deactivate(ctx, id)
		case "reactivate":
			err = client.Reactivate(ctx, id)
		case "grant-admin":
			err = client.SetAdmin(ctx, id, true)
		case "revoke-admin":
			err = client.SetAdmin(ctx, id, false)
		case "set-password":
			var body struct {
				Password string `json:"password"`
			}
			if decErr := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); decErr != nil {
				Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
				return
			}
			err = client.SetPassword(ctx, id, body.Password)
			// Dropped as early as possible. It is never logged, never echoed and
			// never put in a path; the audit middleware records no bodies at all.
			body.Password = ""
		default:
			Error(w, http.StatusNotFound, "Unbekannte Aktion.")
			return
		}

		if err != nil {
			var actErr *mas.ActionError
			if errors.As(err, &actErr) {
				status := http.StatusBadGateway
				switch {
				case actErr.NotFound():
					status = http.StatusNotFound
				case actErr.Rejected():
					status = http.StatusBadRequest
				case actErr.Forbidden():
					status = http.StatusForbidden
				}
				Error(w, status, actErr.Msg)
				return
			}
			Error(w, http.StatusBadGateway, "MAS antwortet nicht: "+err.Error())
			return
		}

		JSON(w, http.StatusOK, map[string]any{"ok": true, "action": action})
	}
}

// isActingUser resolves the session's identity through MAS and compares it with the
// target.
//
// It resolves rather than string-compares because the session stores whatever the
// OIDC exchange returned — `matrix_user_id` when userinfo provides it, the `sub`
// (a ULID) otherwise. Comparing the raw stored value against a ULID would protect in
// one deployment and silently not in the next.
func (h *UsersHandler) isActingUser(ctx context.Context, r *http.Request, client *mas.Client, targetID string) (bool, error) {
	identity := middleware.UserIDFromContext(r.Context())
	if identity == "" {
		return false, fmt.Errorf("no authenticated identity in context")
	}

	me, err := client.ResolveUser(ctx, identity)
	if err != nil {
		return false, err
	}
	if me == nil {
		// The panel admits MAS admins, so an identity MAS does not know should not
		// be here at all. Treated as unresolvable rather than as "not you".
		return false, fmt.Errorf("MAS does not know the acting identity")
	}
	return me.ID == targetID, nil
}
