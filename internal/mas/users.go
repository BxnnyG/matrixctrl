package mas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// User is one MAS account, flattened out of the JSON:API envelope MAS returns.
//
// Locked and deactivated arrive as timestamps and are **not** the same thing:
// locked is reversible and usually temporary, deactivated is the account being
// gone. Rendering both as "disabled" would throw away the distinction an operator
// needs in order to act, so both survive to the UI.
type User struct {
	ID            string     `json:"id"`
	Username      string     `json:"username"`
	CreatedAt     time.Time  `json:"created_at"`
	LockedAt      *time.Time `json:"locked_at,omitempty"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`
	Admin         bool       `json:"admin"`
	LegacyGuest   bool       `json:"legacy_guest"`
}

// State collapses the timestamps into the one word a list column can hold, without
// losing which of the two it was.
func (u User) State() string {
	switch {
	case u.DeactivatedAt != nil:
		return "deactivated"
	case u.LockedAt != nil:
		return "locked"
	default:
		return "active"
	}
}

// UserPage is one cursor page.
//
// MAS pages by cursor, not offset — there is no page number to show, and inventing
// one in the client would be a lie told in the UI. Next and Prev are opaque IDs to
// be handed back verbatim.
type UserPage struct {
	Users []User `json:"users"`
	// Total is the count across the whole filtered set, when MAS reports it.
	Total int `json:"total"`
	// Next and Prev are cursors, empty when there is nothing in that direction.
	Next string `json:"next,omitempty"`
	Prev string `json:"prev,omitempty"`
}

// UserQuery is the filter set MAS actually supports, and nothing more. A field here
// that MAS ignores would be a control in the UI that silently does nothing.
type UserQuery struct {
	Search string
	// Status is MAS's own vocabulary: "active", "locked", "deactivated". Empty means
	// no filter.
	Status string
	// AdminOnly filters to users who may request admin.
	AdminOnly bool
	// After and Before are cursors from a previous page.
	After  string
	Before string
	Limit  int
}

const (
	defaultLimit = 25
	maxLimit     = 100
)

// ListUsers fetches one page.
func (c *Client) ListUsers(ctx context.Context, q UserQuery) (*UserPage, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	query := url.Values{"count": {"true"}}
	if q.Search != "" {
		query.Set("filter[search]", q.Search)
	}
	if q.Status != "" {
		query.Set("filter[status]", q.Status)
	}
	if q.AdminOnly {
		query.Set("filter[admin]", "true")
	}

	// Paging backwards uses page[last] with page[before]; mixing first/last is a
	// 400 from MAS rather than a merge of the two.
	switch {
	case q.Before != "":
		query.Set("page[before]", q.Before)
		query.Set("page[last]", strconv.Itoa(limit))
	default:
		if q.After != "" {
			query.Set("page[after]", q.After)
		}
		query.Set("page[first]", strconv.Itoa(limit))
	}

	raw, status, err := c.get(ctx, "/api/admin/v1/users", query)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		// The cached token may simply have been revoked. Drop it and try once more,
		// so a rotated secret costs one retry instead of every request until restart.
		c.invalidateToken()
		raw, status, err = c.get(ctx, "/api/admin/v1/users", query)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("mas admin API returned %d", status)
	}

	return parseUserPage(raw)
}

// ErrNotConfigured is returned when MAS admin credentials are absent — bootstrap
// mode, mainly. Distinct from a transport failure so the UI can explain rather than
// show an error nobody can act on.
var ErrNotConfigured = fmt.Errorf("MAS admin access is not configured")

// masUserDoc is the JSON:API envelope MAS returns. Kept private: everything outside
// this package deals in User, not in the wire shape.
type masUserDoc struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Username      string  `json:"username"`
			CreatedAt     string  `json:"created_at"`
			LockedAt      *string `json:"locked_at"`
			DeactivatedAt *string `json:"deactivated_at"`
			Admin         bool    `json:"admin"`
			LegacyGuest   bool    `json:"legacy_guest"`
		} `json:"attributes"`
	} `json:"data"`
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
	} `json:"links"`
}

// parseUserPage flattens the envelope.
//
// Tolerant about individual fields — a MAS version that adds or renames an
// attribute should cost that attribute, not the page — but never invents a user: an
// entry with no ID is skipped, because a row the operator cannot act on is worse
// than a row that is not there.
func parseUserPage(raw []byte) (*UserPage, error) {
	var doc masUserDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unreadable response from MAS: %w", err)
	}

	page := &UserPage{
		Users: make([]User, 0, len(doc.Data)),
		Total: doc.Meta.Count,
		Next:  cursorFromLink(doc.Links.Next, "page[after]"),
		Prev:  cursorFromLink(doc.Links.Prev, "page[before]"),
	}

	for _, d := range doc.Data {
		if d.ID == "" {
			continue
		}
		u := User{
			ID:          d.ID,
			Username:    d.Attributes.Username,
			Admin:       d.Attributes.Admin,
			LegacyGuest: d.Attributes.LegacyGuest,
		}
		if t, ok := parseTime(&d.Attributes.CreatedAt); ok {
			u.CreatedAt = t
		}
		if t, ok := parseTime(d.Attributes.LockedAt); ok {
			u.LockedAt = &t
		}
		if t, ok := parseTime(d.Attributes.DeactivatedAt); ok {
			u.DeactivatedAt = &t
		}
		page.Users = append(page.Users, u)
	}
	return page, nil
}

func parseTime(s *string) (time.Time, bool) {
	if s == nil || *s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// cursorFromLink pulls the cursor out of the link MAS returns, which is a full
// relative URL with the whole query string on it. Handing that back to the client
// and replaying it would mean the client could ask MAS for anything the link
// happened to contain; taking only the cursor keeps the filters under this
// package's control.
func cursorFromLink(link, param string) string {
	if link == "" {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Query().Get(param)
}

// UserByID fetches one user. A 404 is not an error: on the login path it means the
// account is unknown to MAS, which is a legitimate answer to "may this person
// administer the panel" rather than a failure to determine it.
func (c *Client) UserByID(ctx context.Context, id string) (*User, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}

	raw, status, err := c.get(ctx, "/api/admin/v1/users/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.invalidateToken()
		raw, status, err = c.get(ctx, "/api/admin/v1/users/"+url.PathEscape(id), nil)
		if err != nil {
			return nil, err
		}
	}
	switch status {
	case http.StatusNotFound:
		return nil, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("mas admin API returned %d", status)
	}

	return singleUser(raw)
}

// singleUser flattens the one-resource envelope. Shared with the by-username
// lookup: two copies of the same envelope would drift the day MAS adds a field.
func singleUser(raw []byte) (*User, error) {
	var doc struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Username      string  `json:"username"`
				CreatedAt     string  `json:"created_at"`
				LockedAt      *string `json:"locked_at"`
				DeactivatedAt *string `json:"deactivated_at"`
				Admin         bool    `json:"admin"`
				LegacyGuest   bool    `json:"legacy_guest"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unreadable response from MAS: %w", err)
	}
	if doc.Data.ID == "" {
		return nil, nil
	}

	u := &User{
		ID:          doc.Data.ID,
		Username:    doc.Data.Attributes.Username,
		Admin:       doc.Data.Attributes.Admin,
		LegacyGuest: doc.Data.Attributes.LegacyGuest,
	}
	if t, ok := parseTime(&doc.Data.Attributes.CreatedAt); ok {
		u.CreatedAt = t
	}
	if t, ok := parseTime(doc.Data.Attributes.LockedAt); ok {
		u.LockedAt = &t
	}
	if t, ok := parseTime(doc.Data.Attributes.DeactivatedAt); ok {
		u.DeactivatedAt = &t
	}
	return u, nil
}
