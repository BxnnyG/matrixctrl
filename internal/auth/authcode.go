package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// One-time codes exist because the session JWT used to travel in a URL.
//
// The OIDC callback redirected to `/auth/callback?token=<jwt>` and chi's request
// logger writes the full URL, so the token was written to the application log by the
// very request that delivered it — and from there to whatever collects logs. Browser
// history and the Referer header were the same exposure by other routes (P0-5).
//
// The replacement has two independent properties, and it needs both:
//
//  1. The code travels in the URL **fragment**, which browsers never send to a
//     server. Nothing to log, nothing in Referer. A query parameter would still be
//     logged even if it only carried a code.
//  2. The code is single-use and short-lived, so the copy left in browser history is
//     spent by the time anyone reads it.

const (
	// codeTTL is the window between the redirect and the SPA's exchange call —
	// one page load. A minute is generous for that and short for anything else.
	codeTTL = 60 * time.Second
	// codeBytes of entropy. The code is a bearer credential for its lifetime, so it
	// is sized like one rather than like an identifier.
	codeBytes = 32
)

// AuthCodes issues and redeems the one-time codes.
type AuthCodes struct {
	db *pgxpool.Pool
}

func NewAuthCodes(db *pgxpool.Pool) *AuthCodes { return &AuthCodes{db: db} }

// Issue creates a code for a user who has already been authenticated.
//
// The IP and user agent are carried through so the session created at redemption
// records where the login actually came from, rather than where the exchange call
// came from — they are the same browser, but only the first one went through OIDC.
func (a *AuthCodes) Issue(ctx context.Context, userID, ip, userAgent string) (string, error) {
	raw := make([]byte, codeBytes)
	if _, err := rand.Read(raw); err != nil {
		// Same reasoning as the JWT key: a credential that is not random is not a
		// credential. Refusing to issue one fails the login, which is recoverable;
		// issuing a guessable one is not.
		return "", fmt.Errorf("could not generate a login code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(raw)

	_, err := a.db.Exec(ctx, `
		INSERT INTO auth_codes(code, user_id, ip_addr, user_agent, expires_at)
		VALUES($1, $2, $3, $4, NOW() + $5::interval)`,
		code, userID, ip, userAgent, fmt.Sprintf("%d seconds", int(codeTTL.Seconds())),
	)
	if err != nil {
		return "", fmt.Errorf("could not store login code: %w", err)
	}
	return code, nil
}

// Redeemed is what a code was worth.
type Redeemed struct {
	UserID    string
	IP        string
	UserAgent string
}

// Redeem consumes a code exactly once.
//
// The delete and the read are one statement, mirroring how the OIDC state is
// consumed: a check-then-delete would let two requests arriving together both pass,
// and a code that can be redeemed twice is a code that can be replayed.
//
// An expired or unknown code is one error, not two. Distinguishing them would tell a
// caller whether a code ever existed, which is information only an attacker wants.
func (a *AuthCodes) Redeem(ctx context.Context, code string) (*Redeemed, error) {
	if code == "" {
		return nil, fmt.Errorf("invalid or expired login code")
	}

	var out Redeemed
	var ip, ua *string
	err := a.db.QueryRow(ctx, `
		DELETE FROM auth_codes
		WHERE code = $1 AND expires_at > NOW()
		RETURNING user_id, ip_addr, user_agent`,
		code,
	).Scan(&out.UserID, &ip, &ua)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired login code")
	}
	if ip != nil {
		out.IP = *ip
	}
	if ua != nil {
		out.UserAgent = *ua
	}
	return &out, nil
}

// Sweep removes codes nobody redeemed. Without it the table grows by one row per
// abandoned login forever — small, but unbounded is unbounded.
func (a *AuthCodes) Sweep(ctx context.Context) error {
	_, err := a.db.Exec(ctx, `DELETE FROM auth_codes WHERE expires_at < NOW() - INTERVAL '1 hour'`)
	return err
}
