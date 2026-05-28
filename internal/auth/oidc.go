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
)

// OIDCConfig holds all config needed for the OIDC authorization-code flow.
type OIDCConfig struct {
	ClientID     string
	ClientSecret string
	// Issuer is the base URL of MAS, e.g. https://mas-matrix.bxnny.de
	Issuer      string
	RedirectURI string
	// AllowedUsers is a comma-separated list of Matrix IDs allowed to log in.
	// Empty = allow any authenticated OIDC user.
	AllowedUsers []string
}

type OIDCService struct {
	cfg    OIDCConfig
	db     *pgxpool.Pool
	jwtKey []byte
}

func NewOIDCService(cfg OIDCConfig, db *pgxpool.Pool, jwtKey []byte) *OIDCService {
	return &OIDCService{cfg: cfg, db: db, jwtKey: jwtKey}
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
		"scope":         {"openid profile"},
		"state":         {state},
	}
	return o.cfg.Issuer + "/oauth2/authorize?" + params.Encode(), nil
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

	// Token exchange
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {o.cfg.RedirectURI},
		"client_id":     {o.cfg.ClientID},
		"client_secret": {o.cfg.ClientSecret},
	}
	resp, err := http.PostForm(o.cfg.Issuer+"/oauth2/token", body)
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
	req, err := http.NewRequestWithContext(ctx, "GET", o.cfg.Issuer+"/oauth2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	uiResp, err := http.DefaultClient.Do(req)
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

	// Allowlist check
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
	}

	return mxid, nil
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
