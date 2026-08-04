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

// AuthURL generates a state, persists it, and returns the MAS authorization URL.
func (o *OIDCService) AuthURL(ctx context.Context) (string, error) {
	state := uuid.New().String()
	_, err := o.db.Exec(ctx,
		`INSERT INTO oidc_states(state, expires_at) VALUES($1,$2)`,
		state, time.Now().Add(5*time.Minute),
	)
	if err != nil {
		return "", fmt.Errorf("store oidc state: %w", err)
	}

	params := url.Values{
		"response_type": {"code"},
		"client_id":     {o.cfg.ClientID},
		"redirect_uri":  {o.cfg.RedirectURI},
		"scope":         {"openid email"},
		"state":         {state},
	}
	return o.discovery.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// ExchangeCode validates the state, exchanges the code for a token, and returns
// the Matrix user ID extracted from userinfo.
func (o *OIDCService) ExchangeCode(ctx context.Context, code, state string) (string, error) {
	// Consume state (atomic delete, returns error if not found / expired)
	var dummy bool
	err := o.db.QueryRow(ctx,
		`DELETE FROM oidc_states WHERE state=$1 AND expires_at > NOW() RETURNING true`,
		state,
	).Scan(&dummy)
	if err != nil {
		return "", fmt.Errorf("invalid or expired state — please try again")
	}

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
