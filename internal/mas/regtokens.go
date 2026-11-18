package mas

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

// RegistrationToken is an invitation code (etappe 125). MAS calls it a user
// registration token; to an operator it is the thing they hand to a person.
type RegistrationToken struct {
	ID         string     `json:"id"`
	Token      string     `json:"token"`
	Valid      bool       `json:"valid"`
	UsageLimit *int       `json:"usage_limit"` // nil = unlimited
	TimesUsed  int        `json:"times_used"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// State collapses the timestamps into one word, strongest reason first: a revoked code
// is revoked whether or not it has also expired. "valid", "used", "expired", "revoked".
func (t RegistrationToken) State() string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()):
		return "expired"
	case t.UsageLimit != nil && t.TimesUsed >= *t.UsageLimit:
		return "used"
	case !t.Valid:
		// MAS says no for a reason this client does not know; never show it as usable.
		return "expired"
	}
	return "valid"
}

const (
	regTokensPath = "/api/admin/v1/user-registration-tokens"
	// maxTokenPages bounds the walk over all pages: a cursor that never ends must not
	// turn one request into an endless loop against MAS.
	maxTokenPages = 50
)

// regFilters is the filter vocabulary MAS has for this list.
var regFilters = map[string]bool{"used": true, "revoked": true, "expired": true, "valid": true}

type masTokenAttrs struct {
	Token      string  `json:"token"`
	Valid      bool    `json:"valid"`
	UsageLimit *int    `json:"usage_limit"`
	TimesUsed  int     `json:"times_used"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func (a masTokenAttrs) token(id string) RegistrationToken {
	t := RegistrationToken{ID: id, Token: a.Token, Valid: a.Valid, UsageLimit: a.UsageLimit, TimesUsed: a.TimesUsed}
	if c, ok := parseTime(&a.CreatedAt); ok {
		t.CreatedAt = c
	}
	if c, ok := parseTime(a.LastUsedAt); ok {
		t.LastUsedAt = &c
	}
	if c, ok := parseTime(a.ExpiresAt); ok {
		t.ExpiresAt = &c
	}
	if c, ok := parseTime(a.RevokedAt); ok {
		t.RevokedAt = &c
	}
	return t
}

// ListRegistrationTokens returns every code, following the cursor to the end. Codes
// are a handful, not thousands; a page selector for them would be ceremony. filter is
// one of used|revoked|expired|valid or empty.
func (c *Client) ListRegistrationTokens(ctx context.Context, filter string) ([]RegistrationToken, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	if filter != "" && !regFilters[filter] {
		return nil, &ActionError{Status: http.StatusBadRequest, Msg: "Unbekannter Filter."}
	}
	out := []RegistrationToken{}
	after := ""
	for i := 0; i < maxTokenPages; i++ {
		query := url.Values{"page[first]": {strconv.Itoa(maxLimit)}}
		if filter != "" {
			query.Set("filter["+filter+"]", "true")
		}
		if after != "" {
			query.Set("page[after]", after)
		}
		raw, status, err := c.get(ctx, regTokensPath, query)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized {
			c.invalidateToken()
			raw, status, err = c.get(ctx, regTokensPath, query)
			if err != nil {
				return nil, err
			}
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("mas admin API returned %d", status)
		}
		var doc struct {
			Data []struct {
				ID         string        `json:"id"`
				Attributes masTokenAttrs `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("unreadable response from MAS: %w", err)
		}
		for _, d := range doc.Data {
			if d.ID == "" {
				continue // a row nobody can act on
			}
			out = append(out, d.Attributes.token(d.ID))
		}
		next := cursorFromLink(doc.Links.Next, "page[after]")
		if next == "" || next == after {
			return out, nil
		}
		after = next
	}
	return out, nil
}

// CreateRegistrationToken makes a code. Empty token = MAS picks a random one; nil
// usageLimit / expiresAt = unlimited / never.
func (c *Client) CreateRegistrationToken(ctx context.Context, token string, usageLimit *int, expiresAt *time.Time) (*RegistrationToken, error) {
	body := map[string]any{}
	if t := strings.TrimSpace(token); t != "" {
		body["token"] = t
	}
	if err := checkLimit(usageLimit); err != nil {
		return nil, err
	}
	if usageLimit != nil {
		body["usage_limit"] = *usageLimit
	}
	if expiresAt != nil {
		body["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	return c.tokenCall(ctx, http.MethodPost, regTokensPath, body, "create")
}

// EditRegistrationToken replaces limit and expiry. Both are always sent: MAS reads a
// missing field as "clear it", and so does this signature — nil means no limit / no end.
func (c *Client) EditRegistrationToken(ctx context.Context, id string, usageLimit *int, expiresAt *time.Time) (*RegistrationToken, error) {
	if err := checkLimit(usageLimit); err != nil {
		return nil, err
	}
	body := map[string]any{"usage_limit": nil, "expires_at": nil}
	if usageLimit != nil {
		body["usage_limit"] = *usageLimit
	}
	if expiresAt != nil {
		body["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	return c.tokenCall(ctx, http.MethodPut, regTokensPath+"/"+url.PathEscape(id), body, "edit")
}

// RevokeRegistrationToken stops a code from working; UnrevokeRegistrationToken undoes it.
func (c *Client) RevokeRegistrationToken(ctx context.Context, id string) (*RegistrationToken, error) {
	return c.tokenCall(ctx, http.MethodPost, regTokensPath+"/"+url.PathEscape(id)+"/revoke", nil, "revoke")
}

func (c *Client) UnrevokeRegistrationToken(ctx context.Context, id string) (*RegistrationToken, error) {
	return c.tokenCall(ctx, http.MethodPost, regTokensPath+"/"+url.PathEscape(id)+"/unrevoke", nil, "unrevoke")
}

func checkLimit(n *int) error {
	if n != nil && *n < 1 {
		return &ActionError{Status: http.StatusBadRequest, Msg: "Die Zahl der Nutzungen muss mindestens 1 sein."}
	}
	return nil
}

// tokenCall performs one write and reads the resulting token. One 401 retry, like
// every other call in this package.
func (c *Client) tokenCall(ctx context.Context, method, path string, body map[string]any, op string) (*RegistrationToken, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	status, raw, err := c.sendJSON(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.invalidateToken()
		status, raw, err = c.sendJSON(ctx, method, path, body)
		if err != nil {
			return nil, err
		}
	}
	switch status {
	case http.StatusOK, http.StatusCreated:
		var doc struct {
			Data struct {
				ID         string        `json:"id"`
				Attributes masTokenAttrs `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("unreadable response from MAS: %w", err)
		}
		if doc.Data.ID == "" {
			return nil, fmt.Errorf("MAS antwortete ohne Code")
		}
		t := doc.Data.Attributes.token(doc.Data.ID)
		return &t, nil
	case http.StatusNotFound:
		return nil, &ActionError{Status: status, Msg: "MAS kennt diesen Code nicht (mehr)."}
	case http.StatusConflict:
		if op == "create" {
			return nil, &ActionError{Status: status, Msg: "Diesen Code gibt es bereits."}
		}
		return nil, &ActionError{Status: status, Msg: "Der Code ist schon in diesem Zustand."}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return nil, &ActionError{Status: http.StatusBadRequest, Msg: "MAS hat die Eingabe abgelehnt."}
	default:
		return nil, &ActionError{Status: status, Msg: fmt.Sprintf("MAS antwortete mit %d.", status)}
	}
}

// sendJSON is doPostJSON for any method; the existing helper is POST-only and the code
// edit is a PUT.
func (c *Client) sendJSON(ctx context.Context, method, path string, body map[string]any) (int, []byte, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.issuer+path, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("mas admin API: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("mas admin API: %w", err)
	}
	return resp.StatusCode, raw, nil
}
