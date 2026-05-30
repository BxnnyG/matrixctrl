package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math/big"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB-backed OIDC configuration. Lets MatrixCtrl be switched from bootstrap auth to
// OIDC at runtime (Phase 1.5) without editing env/secrets — the connect-OIDC flow
// writes here and the auth service hot-reloads.

const (
	keyOIDCIssuer   = "oidc.issuer"
	keyOIDCClientID = "oidc.client_id"
	keyOIDCSecret   = "oidc.client_secret"
	keyOIDCRedirect = "oidc.redirect_uri"
)

// SaveOIDCConfig persists OIDC settings into instance_settings.
func SaveOIDCConfig(ctx context.Context, db *pgxpool.Pool, cfg OIDCConfig) error {
	pairs := map[string]string{
		keyOIDCIssuer:   cfg.Issuer,
		keyOIDCClientID: cfg.ClientID,
		keyOIDCSecret:   cfg.ClientSecret,
		keyOIDCRedirect: cfg.RedirectURI,
	}
	for k, v := range pairs {
		_, err := db.Exec(ctx, `
			INSERT INTO instance_settings(key, value) VALUES($1,$2)
			ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=NOW()`, k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

// LoadOIDCConfig reads persisted OIDC settings. ok=false if no client_id is stored.
func LoadOIDCConfig(ctx context.Context, db *pgxpool.Pool) (OIDCConfig, bool) {
	rows, err := db.Query(ctx,
		"SELECT key, value FROM instance_settings WHERE key LIKE 'oidc.%'")
	if err != nil {
		return OIDCConfig{}, false
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	cfg := OIDCConfig{
		Issuer:       m[keyOIDCIssuer],
		ClientID:     m[keyOIDCClientID],
		ClientSecret: m[keyOIDCSecret],
		RedirectURI:  m[keyOIDCRedirect],
		RequireAdmin: true,
	}
	return cfg, cfg.ClientID != ""
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// GenerateULID returns a 26-char Crockford base32 string from 128 random bits —
// the client_id format MAS requires for static clients.
func GenerateULID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	v := new(big.Int).SetBytes(b)
	base := big.NewInt(32)
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		mod := new(big.Int)
		v.DivMod(v, base, mod)
		out[i] = crockford[mod.Int64()]
	}
	return string(out)
}

// GenerateSecret returns a 64-char hex client secret (32 random bytes).
func GenerateSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
