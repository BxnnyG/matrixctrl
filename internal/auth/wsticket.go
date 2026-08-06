package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// WebSocket tickets exist because a browser cannot set an Authorization header on a
// WebSocket handshake, so something has to travel in the URL — and a URL is logged.
//
// It was: on 2026-08-06 the session JWT appeared in the container log verbatim, once
// per upgrade stream. E29 had already removed the same token from the OIDC callback
// URL and narrowed `?token=` to WebSocket upgrades, but narrowing where a credential
// may appear in a URL is not the same as stopping it from being logged — and the log
// is not the only reader. The URL also passes the ingress, the tunnel, and any proxy
// in between, each with its own access log.
//
// So the thing in the URL is now worth nothing after one use.
//
// **Deliberately not AuthCodes**, despite being the same shape (random, single-use,
// atomic redemption). Those codes are redeemable at /auth/exchange for a full
// session; sharing the store would let a leaked ticket be traded for one, turning a
// read-only log stream into a complete session. The separation *is* the security
// property, so it is worth the small duplication.
//
// In-memory rather than Postgres: a WebSocket connects to the process that issued its
// ticket, its lifetime is one handshake, and a restart voiding outstanding tickets is
// correct rather than a limitation — the client reconnects and asks for a new one.

const (
	// ticketTTL is the gap between "the page asked for a ticket" and "the browser
	// opened the socket". That is one round trip; anything longer is not a
	// reconnect, it is a replay.
	ticketTTL = 30 * time.Second
	// ticketBytes of entropy — a bearer credential for its lifetime, sized like one.
	ticketBytes = 32
	// maxTickets bounds the store. Tickets are cheap and short-lived, but an
	// authenticated client looping on the issue endpoint must not be able to grow
	// the process without limit.
	maxTickets = 1024
)

type ticket struct {
	userID  string
	expires time.Time
}

// WSTickets issues and redeems single-use WebSocket tickets.
type WSTickets struct {
	mu      sync.Mutex
	tickets map[string]ticket
	now     func() time.Time // injectable for tests
}

func NewWSTickets() *WSTickets {
	return &WSTickets{tickets: map[string]ticket{}, now: time.Now}
}

// Issue returns a ticket for an already-authenticated user.
func (w *WSTickets) Issue(userID string) (string, error) {
	if userID == "" {
		// A ticket that belongs to nobody would authenticate as nobody, which the
		// redeem path would then have to special-case. Refuse at the source.
		return "", fmt.Errorf("cannot issue a ticket without a user")
	}

	raw := make([]byte, ticketBytes)
	if _, err := rand.Read(raw); err != nil {
		// Same reasoning as the JWT key and the login codes: a credential that is
		// not random is not a credential. Failing the request is recoverable.
		return "", fmt.Errorf("could not generate a ticket: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(raw)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.sweepLocked()
	if len(w.tickets) >= maxTickets {
		return "", fmt.Errorf("too many outstanding tickets")
	}
	w.tickets[id] = ticket{userID: userID, expires: w.now().Add(ticketTTL)}
	return id, nil
}

// Redeem spends a ticket and returns the user it was issued to.
//
// Single-use: the entry is removed whether or not it had expired, so a replay of a
// logged ticket finds nothing regardless of timing.
func (w *WSTickets) Redeem(id string) (string, bool) {
	if id == "" {
		return "", false
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	t, ok := w.tickets[id]
	if !ok {
		return "", false
	}
	delete(w.tickets, id)

	if w.now().After(t.expires) {
		return "", false
	}
	return t.userID, true
}

// sweepLocked drops expired entries. Called on issue, which is the only path that
// grows the map, so the store cannot accumulate expired tickets while idle.
func (w *WSTickets) sweepLocked() {
	now := w.now()
	for id, t := range w.tickets {
		if now.After(t.expires) {
			delete(w.tickets, id)
		}
	}
}
