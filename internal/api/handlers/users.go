package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

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
