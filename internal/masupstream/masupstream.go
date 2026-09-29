// Package masupstream manages the external login providers of MAS — "Mit Google
// anmelden" and friends (etappe 110).
//
// One place for sign-in: Element and MatrixCtrl both authenticate through MAS, so a
// provider configured here is available to both, and MatrixCtrl still admits only MAS
// admins (§4.5). What MAS reads is rendered from a small model, with the safety
// decisions made here once instead of in every hand-written config:
//
//   - The Matrix username never comes from the provider (`localpart: ignore`). A
//     provider name equal to an existing account is then just a suggestion the user
//     cannot take, not a takeover (MAS documents `on_conflict` other than `fail` as an
//     account-takeover risk).
//   - The e-mail address is offered, not forced, and never used to link accounts.
//     Existing accounts link themselves from MAS's account page, signed in.
//   - Client secrets live in a Kubernetes Secret that MAS mounts through the chart's
//     `additional.<key>.configSecret`, never in the settings repository.
//   - A provider is switched off with `enabled: false`, never removed: MAS keeps removed
//     entries in its database until `config sync --prune`, so deleting it from the
//     config would leave it half there.
package masupstream

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Kind is the sort of provider. Each has its own endpoints and claim mapping.
type Kind string

const (
	Google Kind = "google"
	GitHub Kind = "github"
	OIDC   Kind = "oidc"
)

// Provider is one external login as the assistant keeps it.
type Provider struct {
	// ID is the ULID MAS identifies the provider by. It is part of the callback URL the
	// operator registers at the provider, so it is fixed at creation and never changes.
	ID           string    `json:"id"`
	Kind         Kind      `json:"kind"`
	Name         string    `json:"name"`
	Issuer       string    `json:"issuer,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Enabled      bool      `json:"enabled"`
	Created      time.Time `json:"created"`
}

// Complete reports whether MAS could use the provider. Drafts — created to learn the
// callback URL, credentials not yet entered — are kept but never rendered: MAS refuses
// to start with an empty client ID, and a draft must not take the login down.
func (p Provider) Complete() bool {
	if p.ClientID == "" || p.ClientSecret == "" {
		return false
	}
	return p.Kind != OIDC || p.Issuer != ""
}

// Callback is the redirect URI to register at the provider.
func Callback(masHost, id string) string {
	return "https://" + strings.TrimSuffix(masHost, "/") + "/upstream/callback/" + id
}

// NewULID returns a fresh ULID: 48 bits of milliseconds, 80 random bits, Crockford
// base32. MAS requires the provider id to be one.
func NewULID(now time.Time) (string, error) {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16)
	if _, err := rand.Read(b[6:]); err != nil {
		return "", err
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// 128 bits → 26 characters; the first carries only the top 3 bits.
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		out[i] = alphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out), nil
}

// ValidKind reports whether k is one the assistant knows.
func ValidKind(k Kind) bool { return k == Google || k == GitHub || k == OIDC }

// DefaultName is the button label MAS shows when the operator gives none.
func DefaultName(k Kind) string {
	switch k {
	case Google:
		return "Google"
	case GitHub:
		return "GitHub"
	}
	return "Single Sign-On"
}

// Render produces the MAS configuration for every complete provider — the document
// stored in the Secret and merged into mas-config by the chart.
//
// Marshalled from structures, never built as text: a client secret may contain any
// character, and a quote in it must not be able to end a YAML string.
func Render(providers []Provider) (string, error) {
	list := []map[string]interface{}{}
	for _, p := range providers {
		if !p.Complete() {
			continue
		}
		entry, err := render(p)
		if err != nil {
			return "", err
		}
		list = append(list, entry)
	}
	out, err := yaml.Marshal(map[string]interface{}{
		"upstream_oauth2": map[string]interface{}{"providers": list},
	})
	return string(out), err
}

func render(p Provider) (map[string]interface{}, error) {
	name := p.Name
	if name == "" {
		name = DefaultName(p.Kind)
	}
	e := map[string]interface{}{
		"id":            p.ID,
		"enabled":       p.Enabled,
		"human_name":    name,
		"client_id":     p.ClientID,
		"client_secret": p.ClientSecret,
		// Open registration through the providers (operator decision 2026-09-29).
		"registration_token_required": false,
	}
	claims := map[string]interface{}{
		// The one line this package exists for: see the package comment.
		"localpart": map[string]interface{}{"action": "ignore"},
	}
	switch p.Kind {
	case Google:
		e["brand_name"] = "google"
		e["issuer"] = "https://accounts.google.com"
		e["token_endpoint_auth_method"] = "client_secret_post"
		e["scope"] = "openid profile email"
		claims["displayname"] = map[string]interface{}{"action": "suggest", "template": "{{ user.name }}"}
		claims["email"] = map[string]interface{}{"action": "suggest", "template": "{{ user.email }}"}
		claims["account_name"] = map[string]interface{}{"template": "{{ user.email }}"}
	case GitHub:
		// GitHub speaks OAuth 2.0, not OIDC: no discovery, profile from the API.
		e["brand_name"] = "github"
		e["discovery_mode"] = "disabled"
		e["fetch_userinfo"] = true
		e["token_endpoint_auth_method"] = "client_secret_post"
		e["authorization_endpoint"] = "https://github.com/login/oauth/authorize"
		e["token_endpoint"] = "https://github.com/login/oauth/access_token"
		e["userinfo_endpoint"] = "https://api.github.com/user"
		e["scope"] = "read:user"
		claims["subject"] = map[string]interface{}{"template": "{{ userinfo_claims.id }}"}
		claims["displayname"] = map[string]interface{}{"action": "suggest", "template": "{{ userinfo_claims.name }}"}
		claims["email"] = map[string]interface{}{"action": "suggest", "template": "{{ userinfo_claims.email }}"}
		claims["account_name"] = map[string]interface{}{"template": "@{{ userinfo_claims.login }}"}
	case OIDC:
		e["issuer"] = p.Issuer
		e["token_endpoint_auth_method"] = "client_secret_basic"
		e["scope"] = "openid profile email"
		// Several providers — Zitadel by default — put only `sub` into the ID token and
		// the profile behind the userinfo endpoint. Without this the name and address
		// offered at registration would be empty. Discovered, so no endpoint to enter.
		e["fetch_userinfo"] = true
		claims["displayname"] = map[string]interface{}{"action": "suggest", "template": "{{ user.name }}"}
		claims["email"] = map[string]interface{}{"action": "suggest", "template": "{{ user.email }}"}
	default:
		return nil, fmt.Errorf("unbekannte Anbieter-Art %q", p.Kind)
	}
	e["claims_imports"] = claims
	return e, nil
}
