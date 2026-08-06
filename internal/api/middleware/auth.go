package middleware

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const UserIDKey contextKey = "user_id"

type TokenValidator func(token string) (userID string, err error)

// TicketRedeemer spends a single-use WebSocket ticket, returning the user it was
// issued to. Nil in tests and anywhere tickets are not wired up.
type TicketRedeemer func(ticket string) (userID string, ok bool)

func RequireAuth(validate TokenValidator) func(http.Handler) http.Handler {
	return RequireAuthWithTickets(validate, nil)
}

// RequireAuthWithTickets authenticates ordinary requests by bearer token and
// WebSocket handshakes by single-use ticket.
//
// The split exists because a browser cannot set an Authorization header on a
// WebSocket, so that one route has to put something in the URL — and URLs are logged,
// by this process and by every proxy on the way. A ticket is spent by the handshake
// it opens, so the copy left in those logs is inert (E35).
func RequireAuthWithTickets(validate TokenValidator, redeem TicketRedeemer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isWebSocketUpgrade(r) && redeem != nil {
				userID, ok := redeem(r.URL.Query().Get("ticket"))
				if !ok {
					http.Error(w, `{"error":"invalid ticket"}`, http.StatusUnauthorized)
					return
				}
				ctx := context.WithValue(r.Context(), UserIDKey, userID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			userID, err := validate(token)
			if err != nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) string {
	// Authorization: Bearer <token>
	auth := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	// No query-parameter fallback, on any route, including WebSocket upgrades.
	//
	// E29 narrowed `?token=` from every route to the WebSocket handshake, because a
	// session token in a URL ends up in links, Referer headers and log lines. The
	// narrowing was not enough: on 2026-08-06 a deploy wrote a valid session JWT to
	// the container log, from the one route still allowed to carry one. WebSocket
	// handshakes now authenticate with a single-use ticket instead
	// (RequireAuthWithTickets), so nothing needs a token in a URL any more (E35).
	return ""
}

// isWebSocketUpgrade tests the request itself rather than a path prefix: a path
// list would have to be kept in step with the router, and the day it is not, the
// query fallback quietly comes back for a route that never wanted it.
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(v), "upgrade") {
			return true
		}
	}
	return false
}

func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}
