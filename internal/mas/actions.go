package mas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Write actions against MAS accounts.
//
// Every one of these does something narrower than its name suggests, and the
// descriptions below are taken from the API's own documentation rather than from
// what the verb implies. They are repeated here because the callers — and the
// confirmation dialogs the callers drive — are the place where a wrong assumption
// turns into an incident handled wrongly.

// ActionError distinguishes the ways a write can fail, so the UI can say which one
// happened instead of "something went wrong".
type ActionError struct {
	Status int
	Msg    string
}

func (e *ActionError) Error() string { return e.Msg }

// Common statuses, named so callers do not compare integers.
func (e *ActionError) NotFound() bool  { return e.Status == http.StatusNotFound }
func (e *ActionError) Rejected() bool  { return e.Status == http.StatusBadRequest }
func (e *ActionError) Forbidden() bool { return e.Status == http.StatusForbidden }

// Lock prevents the user from acting.
//
// It does **not** invalidate existing sessions: MAS's own description says the
// sessions "will work again as soon as they get unlocked". Locking a compromised
// account is therefore not the same as ejecting the attacker, and a caller that
// implies otherwise is misleading someone mid-incident.
func (c *Client) Lock(ctx context.Context, id string) error {
	return c.post(ctx, id, "lock", nil)
}

// Unlock lifts a lock. It does **not** reactivate a deactivated user.
func (c *Client) Unlock(ctx context.Context, id string) error {
	return c.post(ctx, id, "unlock", nil)
}

// Deactivate disables the account, invalidates its sessions and makes it leave all
// rooms.
//
// skip_erase is always true. MAS defaults it to false, i.e. it asks the homeserver
// to GDPR-erase the user — a one-click irreversible erasure, which is the wrong
// default for an admin panel and sits oddly beside a Reactivate that cannot bring
// the data back. Erasure is a workflow with its own consequences and gets its own
// decision, not a hidden default here.
func (c *Client) Deactivate(ctx context.Context, id string) error {
	return c.post(ctx, id, "deactivate", map[string]any{"skip_erase": true})
}

// Reactivate re-enables a deactivated account. It does **not** unlock a locked one.
func (c *Client) Reactivate(ctx context.Context, id string) error {
	return c.post(ctx, id, "reactivate", nil)
}

// SetAdmin grants or revokes the ability to request admin.
//
// Existing sessions keep whatever access they already had, per MAS's description —
// revoking admin does not end a session that is already using it.
func (c *Client) SetAdmin(ctx context.Context, id string, admin bool) error {
	return c.post(ctx, id, "set-admin", map[string]any{"admin": admin})
}

// SetPassword sets a new password.
//
// The password is passed straight through and never logged, never returned and
// never placed in a path. The audit middleware records no request bodies, so it
// cannot reach the audit table either — see internal/api/middleware/audit.go.
func (c *Client) SetPassword(ctx context.Context, id, password string) error {
	if strings.TrimSpace(password) == "" {
		return &ActionError{Status: http.StatusBadRequest, Msg: "Das Passwort darf nicht leer sein."}
	}
	return c.post(ctx, id, "set-password", map[string]any{"password": password})
}

// post performs an authenticated POST to a user action endpoint.
func (c *Client) post(ctx context.Context, id, action string, body map[string]any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	if id == "" {
		return &ActionError{Status: http.StatusBadRequest, Msg: "Keine Benutzer-ID angegeben."}
	}

	path := "/api/admin/v1/users/" + url.PathEscape(id) + "/" + action

	status, err := c.doPost(ctx, path, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		// Same reasoning as the read path: a rotated or revoked secret should cost
		// one retry, not every request until the process restarts.
		c.invalidateToken()
		status, err = c.doPost(ctx, path, body)
		if err != nil {
			return err
		}
	}

	switch status {
	case http.StatusOK, http.StatusNoContent, http.StatusCreated:
		return nil
	case http.StatusNotFound:
		return &ActionError{Status: status, Msg: "MAS kennt dieses Konto nicht (mehr)."}
	case http.StatusBadRequest:
		return &ActionError{Status: status, Msg: "MAS hat die Eingabe abgelehnt — bei Passwörtern meist die Komplexitätsprüfung."}
	case http.StatusForbidden:
		return &ActionError{Status: status, Msg: "MAS erlaubt diese Aktion nicht. Bei Passwörtern heißt das in der Regel, dass Passwort-Anmeldung deaktiviert ist."}
	default:
		return &ActionError{Status: status, Msg: fmt.Sprintf("MAS antwortete mit %d.", status)}
	}
}

func (c *Client) doPost(ctx context.Context, path string, body map[string]any) (int, error) {
	token, err := c.adminToken(ctx)
	if err != nil {
		return 0, err
	}

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return 0, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.issuer+path, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("mas admin API: %w", err)
	}
	defer resp.Body.Close()
	// Drained but not kept: nothing here needs it, and an unread body keeps the
	// connection from being reused. Bounded, like every other read from MAS.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, bodyLimit))
	return resp.StatusCode, nil
}

// ResolveUser finds a user from whichever identifier the session happens to hold.
//
// The OIDC exchange returns `matrix_user_id` from userinfo when MAS provides it and
// the `sub` — a ULID — otherwise, so the stored identity is one of two different
// shapes. Anything comparing identities has to handle both, or it silently works in
// one deployment and not in the next.
//
// Returns (nil, nil) when MAS does not know the identifier.
func (c *Client) ResolveUser(ctx context.Context, identifier string) (*User, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, nil
	}

	if strings.HasPrefix(identifier, "@") {
		localpart := identifier[1:]
		if i := strings.Index(localpart, ":"); i >= 0 {
			localpart = localpart[:i]
		}
		return c.userByUsername(ctx, localpart)
	}
	return c.UserByID(ctx, identifier)
}

func (c *Client) userByUsername(ctx context.Context, username string) (*User, error) {
	raw, status, err := c.get(ctx, "/api/admin/v1/users/by-username/"+url.PathEscape(username), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.invalidateToken()
		raw, status, err = c.get(ctx, "/api/admin/v1/users/by-username/"+url.PathEscape(username), nil)
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
