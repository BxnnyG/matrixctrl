// Package synapse talks to Synapse's admin API.
//
// Separate from internal/mas on purpose. They are different APIs with different
// authentication, different pagination and different error semantics, and the one
// thing they share — "an HTTP client with a bearer token" — is not worth a common
// abstraction that would have to grow an exception for each difference.
//
// Authentication is the *operator's* authority, not the panel's: MatrixCtrl asks MAS
// for the `urn:synapse:admin:*` scope at login, which MAS grants only to accounts with
// `can_request_admin`. The privilege boundary therefore sits in MAS, upstream of
// anything this package could get wrong (etappe 36).
package synapse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	requestTimeout = 15 * time.Second
	// bodyLimit bounds the read. A room list is kilobytes; an unbounded read from a
	// server that is misbehaving is how a status page becomes the outage.
	bodyLimit = 8 << 20
	// defaultLimit is one screenful. Synapse's own default is 100, which is a lot of
	// rows to render for a question usually answered by the first ten.
	defaultLimit = 50
	maxLimit     = 500
)

// Room is one room as the admin API reports it.
type Room struct {
	RoomID         string `json:"room_id"`
	Name           string `json:"name"`
	CanonicalAlias string `json:"canonical_alias"`
	JoinedMembers  int    `json:"joined_members"`
	JoinedLocal    int    `json:"joined_local_members"`
	Version        string `json:"version"`
	Creator        string `json:"creator"`
	Encryption     string `json:"encryption"`
	Federatable    bool   `json:"federatable"`
	Public         bool   `json:"public"`
	JoinRules      string `json:"join_rules"`
	GuestAccess    string `json:"guest_access"`
	HistoryVis     string `json:"history_visibility"`
	StateEvents    int    `json:"state_events"`
	RoomType       string `json:"room_type"`
}

// RoomPage is one page of rooms.
//
// Offset-paginated, unlike MAS's cursors (internal/mas). Synapse hands back a real
// total and explicit next/previous offsets, so the UI can show page numbers here —
// and must not pretend the two subsystems behave alike just because both list things.
type RoomPage struct {
	Rooms      []Room `json:"rooms"`
	Offset     int    `json:"offset"`
	TotalRooms int    `json:"total_rooms"`
	NextBatch  *int   `json:"next_batch"`
	PrevBatch  *int   `json:"prev_batch"`
}

// ListOptions are the query parameters the rooms endpoint understands.
type ListOptions struct {
	From       int
	Limit      int
	SearchTerm string
	OrderBy    string // name, canonical_alias, joined_members, state_events, …
	Dir        string // "f" forward, "b" backward
}

// allowedOrderBy is Synapse's documented set. An unknown value is dropped rather than
// passed through: the server answers 400 for one it does not know, which would turn a
// typo in the frontend into a broken page instead of a default ordering.
var allowedOrderBy = map[string]bool{
	"name": true, "canonical_alias": true, "joined_members": true,
	"joined_local_members": true, "version": true, "creator": true,
	"encryption": true, "federatable": true, "public": true,
	"join_rules": true, "guest_access": true, "history_visibility": true,
	"state_events": true,
}

// Query renders the options as a query string.
func (o ListOptions) Query() string {
	q := url.Values{}

	from := o.From
	if from < 0 {
		from = 0
	}
	q.Set("from", strconv.Itoa(from))

	limit := o.Limit
	switch {
	case limit <= 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}
	q.Set("limit", strconv.Itoa(limit))

	if s := strings.TrimSpace(o.SearchTerm); s != "" {
		q.Set("search_term", s)
	}
	if allowedOrderBy[o.OrderBy] {
		q.Set("order_by", o.OrderBy)
	}
	if o.Dir == "f" || o.Dir == "b" {
		q.Set("dir", o.Dir)
	}
	return q.Encode()
}

// TokenSource hands back the operator's current Synapse-admin access token.
//
// A function rather than a string because the token lives for five minutes and is
// refreshed behind this call; a value captured at construction would be stale by the
// second request.
type TokenSource func(ctx context.Context) (string, error)

// Client is a Synapse admin API client.
type Client struct {
	baseURL string
	token   TokenSource
	http    *http.Client
}

func New(baseURL string, token TokenSource) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Error classifies a failed call so the UI can say something true about it.
//
// The distinction that matters: 401 means "this session has no usable token any more"
// — sign in again — while 403 means "this account is not a Synapse admin", which no
// amount of signing in will fix. Rendering both as "failed to load rooms" would send
// the operator round a loop that cannot terminate.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("synapse returned %d", e.Status)
}

func (e *Error) NeedsLogin() bool { return e.Status == http.StatusUnauthorized }
func (e *Error) NotAdmin() bool   { return e.Status == http.StatusForbidden }

// ListRooms returns one page of rooms.
func (c *Client) ListRooms(ctx context.Context, opts ListOptions) (*RoomPage, error) {
	endpoint := c.baseURL + "/_synapse/admin/v1/rooms?" + opts.Query()

	raw, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var page RoomPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("could not read the room list: %w", err)
	}
	return &page, nil
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, endpoint, nil)
}

// do performs one authenticated admin-API call.
//
// The body is JSON when present. Kept as one method rather than a get and a put
// because everything around the verb — the token check, the bearer header, the
// bounded read, the error classification — is identical, and duplicating it is how
// a second copy ends up missing the "no token in this process" case that the whole
// reconnect flow depends on (etappe 41).
func (c *Client) do(ctx context.Context, method, endpoint string, body any) ([]byte, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	if token == "" {
		// Not an HTTP failure: there is no token in this process at all, which is the
		// normal state after a restart because the refresh token is held in memory
		// only. Reported as 401 so the caller's one branch covers both.
		return nil, &Error{Status: http.StatusUnauthorized, Message: "no Matrix session in this process"}
	}

	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		payload = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("synapse unreachable: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return nil, fmt.Errorf("could not read the response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp.StatusCode, raw)
	}
	return raw, nil
}

// parseError reads Matrix's standard error envelope, falling back to the status alone.
func parseError(status int, body []byte) *Error {
	out := &Error{Status: status}
	var envelope struct {
		ErrCode string `json:"errcode"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		out.Code = envelope.ErrCode
		out.Message = envelope.Error
	}
	if out.Message == "" {
		out.Message = fmt.Sprintf("Synapse antwortete mit %d.", status)
	}
	return out
}
