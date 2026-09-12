package middleware

import (
	"net/http"
)

// readOnlySafe are the non-GET requests that change nothing on this side.
//
// An allowlist, not a denylist. Forgetting an entry here costs a read-only user one
// harmless button; forgetting one in a list of *writes* would hand them the homeserver.
// The asymmetry decides the shape.
//
//   - logout ends the caller's own session. Refusing it would trap somebody in a panel
//     they are not allowed to use.
//   - ws-ticket buys a single-use ticket for the log stream, which is reading.
//   - the two validate endpoints answer "would this be accepted", touching nothing.
//   - restore/preview reads an uploaded archive and reports what is in it. The restore
//     itself is not here.
//
// Deliberately absent: /api/v1/rtc/reachability. It writes nothing either, but it
// leaves the cluster and discloses this installation's public address to two third
// parties. That is an action with an effect outside, and a read-only session does not
// get to take it.
var readOnlySafe = map[string]bool{
	"/api/v1/auth/logout":            true,
	"/api/v1/auth/ws-ticket":         true,
	"/api/v1/config/validate":        true,
	"/api/v1/config/validate-merged": true,
	"/api/v1/status/restore/preview": true,
}

// RequireWrite refuses changes from sessions that may only look.
//
// Until etappe 100 there was one role. Everybody who could sign in could deploy,
// upgrade, rewrite the configuration, restore an archive over the database and
// deactivate accounts. That held while signing in meant knowing the local admin
// password; it stopped holding when MatrixCtrl learned to create MAS admins, because
// `requireAdmin` lets every Matrix admin in — so adding a moderator handed them the
// server (§4.100).
func RequireWrite(mayWrite func(userID string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !auditedMethods[r.Method] || readOnlySafe[r.URL.Path] || mayWrite(UserIDFromContext(r.Context())) {
				next.ServeHTTP(w, r)
				return
			}
			// 403 and not 401: the session is valid, and sending them back to the login
			// screen would suggest signing in again might help. It would not.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"Dieses Konto darf lesen, aber nichts ändern. Ein Administrator kann es in den Chart-Werten unter roles.admins freischalten."}`))
		})
	}
}
