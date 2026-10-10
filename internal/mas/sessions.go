package mas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth2Session is one OAuth 2.0 session MAS holds for a client (etappe 119d).
type OAuth2Session struct {
	ID           string
	ClientID     string
	UserID       string
	Scope        string
	CreatedAt    time.Time
	LastActiveAt *time.Time
	FinishedAt   *time.Time
}

// sessionCap bounds the sweep. A client that has piled up more than this has a problem
// the next sweep continues with.
const sessionCap = 2000

// ActiveOAuth2Sessions lists the active sessions of one client, every page up to the cap.
func (c *Client) ActiveOAuth2Sessions(ctx context.Context, clientID string) ([]OAuth2Session, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	var out []OAuth2Session
	after := ""
	for len(out) < sessionCap {
		q := url.Values{}
		q.Set("filter[client]", clientID)
		q.Set("filter[status]", "active")
		q.Set("page[first]", "100")
		if after != "" {
			q.Set("page[after]", after)
		}
		raw, status, err := c.get(ctx, "/api/admin/v1/oauth2-sessions", q)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized {
			c.invalidateToken()
			if raw, status, err = c.get(ctx, "/api/admin/v1/oauth2-sessions", q); err != nil {
				return nil, err
			}
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("mas admin API returned %d listing sessions", status)
		}
		page, err := parseSessions(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 100 {
			break
		}
		after = page[len(page)-1].ID
	}
	return out, nil
}

func parseSessions(raw []byte) ([]OAuth2Session, error) {
	var doc struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				ClientID     string     `json:"client_id"`
				UserID       *string    `json:"user_id"`
				Scope        string     `json:"scope"`
				CreatedAt    time.Time  `json:"created_at"`
				LastActiveAt *time.Time `json:"last_active_at"`
				FinishedAt   *time.Time `json:"finished_at"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("mas sessions: %w", err)
	}
	out := make([]OAuth2Session, 0, len(doc.Data))
	for _, d := range doc.Data {
		s := OAuth2Session{ID: d.ID, ClientID: d.Attributes.ClientID, Scope: d.Attributes.Scope,
			CreatedAt: d.Attributes.CreatedAt, LastActiveAt: d.Attributes.LastActiveAt, FinishedAt: d.Attributes.FinishedAt}
		if d.Attributes.UserID != nil {
			s.UserID = *d.Attributes.UserID
		}
		out = append(out, s)
	}
	return out, nil
}

// FinishOAuth2Session ends one session; its tokens stop working.
func (c *Client) FinishOAuth2Session(ctx context.Context, id string) error {
	status, err := c.doPost(ctx, "/api/admin/v1/oauth2-sessions/"+url.PathEscape(id)+"/finish", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("mas admin API returned %d finishing session %s", status, id)
	}
	return nil
}

// clientAPIScope marks a session that carries a Matrix grant (rooms, moderation), as
// opposed to a sign-in that only proved who someone is.
const clientAPIScope = "urn:matrix:org.matrix.msc2967.client:api:"

const (
	// LoginSessionGrace: a sign-in's session is useless once MatrixCtrl has its own,
	// which takes seconds. Ten minutes leaves an unfinished sign-in alone.
	LoginSessionGrace = 10 * time.Minute
	// GrantIdleLimit: a Matrix grant in use refreshes every few minutes. One untouched
	// for a day belongs to a process that restarted or a tab that is gone.
	GrantIdleLimit = 24 * time.Hour
	// MachineIdleLimit: MatrixCtrl's own admin access (client credentials, no user) mints
	// a new session with every token, every few minutes. An hour of silence means that
	// token has long expired; the one in use is never that quiet.
	MachineIdleLimit = time.Hour
)

// StaleSessions picks the sessions of clientID that MatrixCtrl has left behind.
func StaleSessions(sessions []OAuth2Session, clientID string, now time.Time) []OAuth2Session {
	var out []OAuth2Session
	for _, s := range sessions {
		if s.ClientID != clientID || s.FinishedAt != nil {
			continue
		}
		last := s.CreatedAt
		if s.LastActiveAt != nil && s.LastActiveAt.After(last) {
			last = *s.LastActiveAt
		}
		if s.UserID == "" {
			if now.Sub(last) > MachineIdleLimit {
				out = append(out, s)
			}
			continue
		}
		if !strings.Contains(s.Scope, clientAPIScope) {
			if now.Sub(s.CreatedAt) > LoginSessionGrace {
				out = append(out, s)
			}
			continue
		}
		if now.Sub(last) > GrantIdleLimit {
			out = append(out, s)
		}
	}
	return out
}
