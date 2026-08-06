package middleware

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// Requests are logged without their credentials.
//
// chi's stock Logger writes the full URL. On 2026-08-06 an operator's own deploy put
// a valid session JWT into the container log, because the upgrade-log WebSocket
// carried `?token=<jwt>`:
//
//	"GET .../upgrade/<id>/logs?token=eyJhbGciOiJIUzI1NiIs… HTTP/1.1"
//
// Anyone who could read the log could take the session. E29 removed the token from
// the OIDC callback URL for exactly this reason and left the WebSocket route, which
// E35 moves to single-use tickets — but the logger is the choke point, so the
// guarantee belongs here too: a future route that puts a secret in a query string
// must not be able to leak it just by being called.

// redactedKeys are query parameters whose values never appear in the log.
//
// Values are replaced rather than dropped, so the log still records that the
// parameter was present — "ticket=[redacted]" is a different fact from no ticket at
// all, and the difference matters when reading back a failed handshake.
var redactedKeys = map[string]bool{
	"token":         true,
	"ticket":        true,
	"code":          true, // OAuth authorization code
	"access_token":  true,
	"refresh_token": true,
	"id_token":      true,
	"client_secret": true,
	"secret":        true,
	"password":      true,
	"api_key":       true,
	"state":         true, // CSRF value; cheap to redact, awkward to have leaked
}

// SanitizeURL renders a URL for logging with credential values removed.
//
// Deliberately keeps every other parameter: `?container=postgres` is how a reader
// works out which container's logs were requested, and blanking the whole query
// string would trade one debugging problem for another.
func SanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.Path
	}

	// Parse manually rather than with url.ParseQuery: a malformed query must still
	// be redacted, and ParseQuery drops what it cannot parse — which would hide a
	// token sitting in a pair it did not like.
	parts := strings.Split(u.RawQuery, "&")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		key, _, hasValue := strings.Cut(p, "=")
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			decoded = key
		}
		switch {
		case redactedKeys[strings.ToLower(decoded)]:
			out = append(out, key+"=[redacted]")
		case hasValue:
			out = append(out, p)
		default:
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return u.Path
	}
	return u.Path + "?" + strings.Join(out, "&")
}

// Logger logs one line per request, with credentials redacted.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()

		defer func() {
			log.Printf("%s %s %s %s - %d %dB in %s",
				r.Method,
				sanitizedHost(r),
				SanitizeURL(r.URL),
				r.Proto,
				ww.Status(),
				ww.BytesWritten(),
				roundDuration(time.Since(started)),
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

func sanitizedHost(r *http.Request) string {
	if r.Host == "" {
		return "-"
	}
	return r.Host
}

// roundDuration keeps the line readable without pretending to sub-microsecond
// precision that nothing here needs.
func roundDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return d.Round(time.Millisecond).String()
	}
}
