// Package mas talks to the Matrix Authentication Service admin API.
//
// It exists as its own package because two callers now need it: the login path,
// which checks whether the person signing in may request admin, and user
// management. Before this package the token minting lived inside
// internal/auth/oidc.go, which was correct while there was exactly one caller and
// would have drifted the day someone changed the scope, the issuer handling or the
// error semantics in one copy and not the other.
//
// Under MSC3861 — which ESS uses — MAS is authoritative for accounts. Synapse keeps
// its own user table and the two can disagree on a migrated stack, so anything
// built on this package reports *MAS users* and should say so rather than implying
// it lists everything Synapse has ever seen.
package mas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	adminScope     = "urn:mas:admin"
	requestTimeout = 15 * time.Second
	// tokenSkew expires the cached token early, so a request is never sent with a
	// token that expires while it is in flight.
	tokenSkew = 30 * time.Second
	// bodyLimit bounds what is read from MAS. A page of users is kilobytes; an
	// unbounded read from a component that could be misbehaving is how an admin
	// panel becomes the outage.
	bodyLimit = 4 << 20
)

// Client is a MAS admin API client. The zero value is not usable; use New.
type Client struct {
	issuer        string
	tokenEndpoint string
	clientID      string
	clientSecret  string
	http          *http.Client

	mu        sync.Mutex
	token     string
	tokenTill time.Time
}

// New builds a client. tokenEndpoint comes from OIDC discovery, which the caller
// has already performed — rediscovering it here would mean a second round trip and
// a second opinion about the same value.
func New(issuer, tokenEndpoint, clientID, clientSecret string) *Client {
	return &Client{
		issuer:        strings.TrimRight(issuer, "/"),
		tokenEndpoint: tokenEndpoint,
		clientID:      clientID,
		clientSecret:  clientSecret,
		http:          &http.Client{Timeout: requestTimeout},
	}
}

// Configured reports whether admin calls are possible at all. Bootstrap mode has no
// OIDC client credentials, so the honest answer there is "this feature needs MAS",
// not an obscure failure at the first request.
func (c *Client) Configured() bool {
	return c != nil && c.tokenEndpoint != "" && c.clientID != "" && c.clientSecret != ""
}

// token returns a cached admin token, minting one if needed.
//
// The previous implementation minted a fresh token for every call, which was fine
// for one check per login and is not fine for a paged list — it would double the
// request count for every page.
func (c *Client) adminToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenTill) {
		return c.token, nil
	}

	body := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {adminScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenEndpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("admin token request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("parse admin token: %w", err)
	}
	if tr.Error != "" {
		return "", fmt.Errorf("admin token error %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("admin token response contained no token")
	}

	c.token = tr.AccessToken
	// A response without expires_in is treated as short-lived rather than eternal:
	// caching a token past its life turns one expiry into a page that fails until
	// the process restarts.
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= tokenSkew {
		ttl = 5 * time.Minute
	}
	c.tokenTill = time.Now().Add(ttl - tokenSkew)

	return c.token, nil
}

// get performs an authenticated GET against a MAS admin path and returns the body.
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return nil, 0, err
	}

	endpoint := c.issuer + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("mas admin API: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// invalidateToken drops the cached token so the next call mints a fresh one. Called
// on a 401, because the alternative — every subsequent request failing until the
// process restarts — is the kind of failure that looks like the feature is broken.
func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.token, c.tokenTill = "", time.Time{}
	c.mu.Unlock()
}
