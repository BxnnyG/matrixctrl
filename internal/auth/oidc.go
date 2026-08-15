package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/mas"
)

// OIDCConfig holds all config needed for the OIDC authorization-code flow.
type OIDCConfig struct {
	ClientID     string
	ClientSecret string
	// Issuer is the base URL of MAS, e.g. https://mas.example.com
	Issuer      string
	RedirectURI string
	// AllowedUsers is an explicit allowlist of MAS user IDs (ULIDs, the OIDC `sub`).
	// If set, it takes priority over RequireAdmin.
	AllowedUsers []string
	// RequireAdmin, when true, queries the MAS Admin API to verify the logged-in user
	// has can_request_admin=true. Only MAS admins can then log in. This auto-tracks
	// admin status — no manual user lists needed. Uses the same ClientID/ClientSecret
	// via a client_credentials grant with the urn:mas:admin scope.
	RequireAdmin bool
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

type OIDCService struct {
	cfg       OIDCConfig
	db        *pgxpool.Pool
	jwtKey    []byte
	discovery *oidcDiscovery
	// mas is the shared MAS admin client. The token minting and the admin lookup
	// used to live in this file, which was correct while login was the only caller.
	// User management is the second one, and two copies would drift the day someone
	// changes the scope or the error semantics in one of them (CLAUDE.md rule 3).
	mas *mas.Client
}

// MAS exposes the shared admin client so the API layer can reuse the same
// credentials and token cache rather than building a second one.
func (o *OIDCService) MAS() *mas.Client { return o.mas }

func NewOIDCService(cfg OIDCConfig, db *pgxpool.Pool, jwtKey []byte) (*OIDCService, error) {
	svc := &OIDCService{cfg: cfg, db: db, jwtKey: jwtKey}
	issuer := strings.TrimRight(cfg.Issuer, "/")
	resp, err := http.Get(issuer + "/.well-known/openid-configuration")
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("oidc discovery parse: %w", err)
	}
	svc.discovery = &d
	// Built here rather than lazily: discovery has just produced the token endpoint,
	// and a second opinion about that value later is a bug waiting for a config change.
	svc.mas = mas.New(cfg.Issuer, d.TokenEndpoint, cfg.ClientID, cfg.ClientSecret)
	return svc, nil
}

func (o *OIDCService) Enabled() bool {
	return o.cfg.ClientID != "" && o.cfg.ClientSecret != "" && o.cfg.Issuer != ""
}

// Scope requested when signing in.
//
// Deliberately unchanged by etappe 36. Adding `urn:synapse:admin:*` here would make
// every login ask for a scope MAS has never granted on this deployment, and if MAS
// rejects such an authorization rather than omitting the scope, nobody can sign in —
// S11 check 3, untestable from the server side. The rooms feature asks for it in its
// own flow instead, where a wrong guess costs that feature and nothing else.
const loginScope = "openid email"

// SynapseAdminScope is what Synapse's admin API requires. MAS grants it only to
// accounts with `can_request_admin`, so the privilege check lives upstream of any
// code here.
//
// Deliberately *not* `urn:matrix:org.matrix.msc2967.client:api:*`: that grants the
// full client-server API and creates a device on the account, which would let this
// panel read the operator's messages. Rooms need the admin API and nothing more.
const SynapseAdminScope = "urn:synapse:admin:*"

const (
	purposeLogin = "login"
	// PurposeSynapseAdmin is exported because the callback handler branches on it.
	PurposeSynapseAdmin = "synapse_admin"
)

// AuthURL generates a state, persists it, and returns the MAS authorization URL.
func (o *OIDCService) AuthURL(ctx context.Context) (string, error) {
	return o.authURL(ctx, loginScope, purposeLogin)
}

// SynapseAdminAuthURL starts the separate authorization that grants the panel the
// operator's Synapse admin authority, for rooms and moderation (E36).
func (o *OIDCService) SynapseAdminAuthURL(ctx context.Context) (string, error) {
	return o.authURL(ctx, "openid "+SynapseAdminScope, PurposeSynapseAdmin)
}

func (o *OIDCService) authURL(ctx context.Context, scope, purpose string) (string, error) {
	state := uuid.New().String()
	_, err := o.db.Exec(ctx,
		`INSERT INTO oidc_states(state, expires_at, purpose) VALUES($1,$2,$3)`,
		state, time.Now().Add(5*time.Minute), purpose,
	)
	if err != nil {
		return "", fmt.Errorf("store oidc state: %w", err)
	}

	params := url.Values{
		"response_type": {"code"},
		"client_id":     {o.cfg.ClientID},
		"redirect_uri":  {o.cfg.RedirectURI},
		"scope":         {scope},
		"state":         {state},
	}
	return o.discovery.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// StatePurpose consumes a state and reports which flow it belongs to.
//
// Both flows return to the same callback because MAS validates redirect_uris strictly
// and the static client is registered with exactly one; adding a second would mean
// editing ESS's MAS config to add a page. The purpose is read from the database, not
// from anything the browser carried, because the state is a CSRF token and a value the
// client could edit is not one to branch authorization on.
func (o *OIDCService) StatePurpose(ctx context.Context, state string) (string, error) {
	var purpose string
	err := o.db.QueryRow(ctx,
		`DELETE FROM oidc_states WHERE state=$1 AND expires_at > NOW() RETURNING purpose`,
		state,
	).Scan(&purpose)
	if err != nil {
		return "", fmt.Errorf("invalid or expired state — please try again")
	}
	return purpose, nil
}

// ExchangeCode validates the state, exchanges the code for a token, and returns
// the Matrix user ID extracted from userinfo.
func (o *OIDCService) ExchangeCode(ctx context.Context, code, state string) (string, error) {
	purpose, err := o.StatePurpose(ctx, state)
	if err != nil {
		return "", err
	}
	if purpose != purposeLogin {
		// A state minted for the rooms authorization must not be redeemable as a
		// login: the two grant different things and the caller asked for one of them.
		return "", fmt.Errorf("invalid or expired state — please try again")
	}
	return o.LoginWithCode(ctx, code)
}

// LoginWithCode exchanges an authorization code for a session, with the state already
// consumed by the caller. Split out of ExchangeCode so the callback can read which
// flow a state belongs to before deciding what to do with the code (E36) — the login
// path below is otherwise unchanged.
func (o *OIDCService) LoginWithCode(ctx context.Context, code string) (string, error) {
	// Token exchange — client_secret_basic: credentials in Authorization header
	body := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {o.cfg.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", o.discovery.TokenEndpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.cfg.ClientID, o.cfg.ClientSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tr.Error != "" {
		return "", fmt.Errorf("token error %s: %s", tr.Error, tr.ErrorDesc)
	}

	// UserInfo
	uiReq, err := http.NewRequestWithContext(ctx, "GET", o.discovery.UserinfoEndpoint, nil)
	if err != nil {
		return "", err
	}
	uiReq.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	uiResp, err := http.DefaultClient.Do(uiReq)
	if err != nil {
		return "", fmt.Errorf("userinfo: %w", err)
	}
	defer uiResp.Body.Close()
	raw, _ = io.ReadAll(uiResp.Body)

	// MAS returns the MXID as the `sub` claim.
	// Some MAS versions also expose it under a custom claim.
	var ui struct {
		Sub          string `json:"sub"`
		MatrixUserID string `json:"https://matrix.org/user_id"`
	}
	if err := json.Unmarshal(raw, &ui); err != nil {
		return "", fmt.Errorf("parse userinfo: %w", err)
	}
	mxid := ui.MatrixUserID
	if mxid == "" {
		mxid = ui.Sub
	}
	if mxid == "" {
		return "", fmt.Errorf("no Matrix user identifier in token response")
	}

	// Allowlist check (explicit list of MAS user IDs takes priority over admin check)
	if len(o.cfg.AllowedUsers) > 0 {
		ok := false
		for _, u := range o.cfg.AllowedUsers {
			if strings.EqualFold(strings.TrimSpace(u), mxid) {
				ok = true
				break
			}
		}
		if !ok {
			return "", fmt.Errorf("user %s is not in the MatrixCtrl allowlist", mxid)
		}
		return mxid, nil
	}

	// MAS admin check: only users with can_request_admin=true may log in.
	if o.cfg.RequireAdmin {
		isAdmin, err := o.isMASAdmin(ctx, mxid)
		if err != nil {
			return "", fmt.Errorf("could not verify admin status: %w", err)
		}
		if !isAdmin {
			return "", fmt.Errorf("nur Admins können sich bei MatrixCtrl anmelden")
		}
	}

	return mxid, nil
}

// isMASAdmin asks MAS whether the given user ID (a ULID, the OIDC `sub`) may
// request admin. An account MAS does not know is not an admin — a definite answer,
// not a failure to determine one.
func (o *OIDCService) isMASAdmin(ctx context.Context, userID string) (bool, error) {
	user, err := o.mas.UserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return user.Admin, nil
}

// CreateOIDCSession creates a DB session for a Matrix user and returns a JWT.
// Re-uses the same session/JWT format as Bootstrap.Login so the rest of the
// auth middleware doesn't need to know how the session was created.
func (o *OIDCService) CreateOIDCSession(ctx context.Context, userID, ipAddr, userAgent string) (string, error) {
	sessionID := uuid.New()
	expiresAt := time.Now().Add(8 * time.Hour)

	_, err := o.db.Exec(ctx, `
		INSERT INTO sessions(id, user_id, expires_at, ip_addr, user_agent)
		VALUES($1,$2,$3,$4,$5)`,
		sessionID, userID, expiresAt, ipAddr, userAgent,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	claims := jwt.MapClaims{
		"sub": userID,
		"sid": sessionID.String(),
		"exp": expiresAt.Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(o.jwtKey)
}

// SynapseAdminTokens exchanges the code from the rooms authorization for the
// operator's Synapse-admin tokens (E36).
//
// Returns the refresh token to the caller, which keeps it in memory and never writes
// it down (see internal/auth/matrixtoken.go). Nothing here logs any of it: an access
// token is a credential, and the error paths deliberately report the OAuth error code
// rather than echoing the response body, which would contain one on a partial failure.
func (o *OIDCService) SynapseAdminTokens(ctx context.Context, code string) (access, refresh string, expiresIn int, err error) {
	body := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {o.cfg.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", o.discovery.TokenEndpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.cfg.ClientID, o.cfg.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", "", 0, fmt.Errorf("parse token response: %w", err)
	}
	if tr.Error != "" {
		return "", "", 0, fmt.Errorf("token error %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return "", "", 0, fmt.Errorf("no access token in the response")
	}
	// MAS may grant fewer scopes than were asked for. Silently keeping a token that
	// cannot reach the admin API would surface later as an unexplained 403 from
	// Synapse, far from the cause.
	if tr.Scope != "" && !strings.Contains(tr.Scope, SynapseAdminScope) {
		return "", "", 0, fmt.Errorf("this account was not granted Synapse admin access")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 300 // measured MAS default; a missing value must not read as "already expired"
	}
	return tr.AccessToken, tr.RefreshToken, tr.ExpiresIn, nil
}

// RefreshSynapseAdmin renews a Synapse-admin access token. Shaped to fit Refresher.
func (o *OIDCService) RefreshSynapseAdmin(ctx context.Context, refreshToken string) (access, refresh string, expiresIn int, err error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", o.discovery.TokenEndpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.cfg.ClientID, o.cfg.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", "", 0, fmt.Errorf("parse refresh response: %w", err)
	}
	if tr.Error != "" || tr.AccessToken == "" {
		// The MAS session is gone — revoked, expired, or signed out elsewhere. The
		// caller drops the entry so this does not become a retry loop.
		return "", "", 0, fmt.Errorf("refresh rejected: %s", tr.Error)
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 300
	}
	return tr.AccessToken, tr.RefreshToken, tr.ExpiresIn, nil
}

// UserID reads the caller's identity from an access token.
//
// The rooms authorization comes back to the *public* callback, which cannot know
// which signed-in operator it belongs to. This resolves that from the token itself —
// and it must derive the identity exactly the way the login path does, or the Matrix
// session would be filed under a key no request ever looks up. Same claims, same
// precedence, one implementation (CLAUDE.md rule 3).
func (o *OIDCService) UserID(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.discovery.UserinfoEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var ui struct {
		Sub          string `json:"sub"`
		MatrixUserID string `json:"https://matrix.org/user_id"`
	}
	if err := json.Unmarshal(raw, &ui); err != nil {
		return "", fmt.Errorf("parse userinfo: %w", err)
	}
	if ui.MatrixUserID != "" {
		return ui.MatrixUserID, nil
	}
	if ui.Sub == "" {
		return "", fmt.Errorf("no Matrix user identifier in token response")
	}
	return ui.Sub, nil
}
