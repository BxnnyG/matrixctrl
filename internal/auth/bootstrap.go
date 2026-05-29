package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const bootstrapUserID = "admin"

var ErrInvalidCredentials = errors.New("invalid credentials")

type Bootstrap struct {
	db     *pgxpool.Pool
	jwtKey []byte
}

func (b *Bootstrap) JWTKey() []byte { return b.jwtKey }

// NewBootstrap wires the auth service and resolves the JWT signing key.
//
// Key resolution order:
//  1. MATRIXCTRL_JWT_SECRET env var (explicit override — useful for multi-replica
//     setups that must share a key, or when injecting via a k8s Secret).
//  2. Persisted key in the instance_settings table (auto-generated on first boot).
//
// This means a fresh install needs ZERO secret configuration: on first start a
// cryptographically-random 32-byte key is generated and stored in Postgres, then
// reused on every subsequent boot. Tokens survive restarts without any env var.
func NewBootstrap(ctx context.Context, db *pgxpool.Pool) *Bootstrap {
	if env := os.Getenv("MATRIXCTRL_JWT_SECRET"); env != "" {
		return &Bootstrap{db: db, jwtKey: []byte(env)}
	}

	key, err := getOrCreateJWTSecret(ctx, db)
	if err != nil {
		// Last-resort fallback so the service still starts; logged loudly.
		log.Printf("WARNING: could not persist JWT secret (%v) — using ephemeral key; tokens will not survive restart", err)
		key = randomKey()
	}
	return &Bootstrap{db: db, jwtKey: key}
}

// getOrCreateJWTSecret reads the persisted JWT key, generating and storing one on
// first run. Uses an atomic INSERT ... ON CONFLICT to be safe across replicas.
func getOrCreateJWTSecret(ctx context.Context, db *pgxpool.Pool) ([]byte, error) {
	newKey := base64.StdEncoding.EncodeToString(randomKey())

	var stored string
	err := db.QueryRow(ctx, `
		INSERT INTO instance_settings(key, value)
		VALUES('jwt_secret', $1)
		ON CONFLICT (key) DO UPDATE SET key = instance_settings.key
		RETURNING value`,
		newKey,
	).Scan(&stored)
	if err != nil {
		return nil, err
	}

	decoded, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		// Stored value isn't base64 (e.g. legacy plain string) — use raw bytes.
		return []byte(stored), nil
	}
	return decoded, nil
}

func randomKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is catastrophic; fall back to a time-seeded value.
		return []byte(fmt.Sprintf("matrixctrl-fallback-%d", time.Now().UnixNano()))
	}
	return b
}

// EnsureAdminExists creates the admin user on first run if not present.
func (b *Bootstrap) EnsureAdminExists(ctx context.Context) error {
	var exists bool
	err := b.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bootstrap_credentials WHERE user_id=$1)", bootstrapUserID).Scan(&exists)
	if err != nil {
		// Table may not exist yet — handled by migration; just skip
		return nil
	}
	if exists {
		return nil
	}

	password := os.Getenv("MATRIXCTRL_ADMIN_PASSWORD")
	if password == "" {
		password = generatePassword()
		log.Printf("MatrixCtrl: bootstrap admin password: %s", password)
		log.Printf("MatrixCtrl: set MATRIXCTRL_ADMIN_PASSWORD env var to override")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	_, err = b.db.Exec(ctx,
		"INSERT INTO bootstrap_credentials(user_id, password_hash) VALUES($1, $2)",
		bootstrapUserID, string(hash),
	)
	return err
}

func (b *Bootstrap) Login(ctx context.Context, username, password, ipAddr, userAgent string) (token string, err error) {
	var hash string
	err = b.db.QueryRow(ctx,
		"SELECT password_hash FROM bootstrap_credentials WHERE user_id=$1", username,
	).Scan(&hash)
	if err != nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	sessionID := uuid.New()
	expiresAt := time.Now().Add(8 * time.Hour)

	_, err = b.db.Exec(ctx, `
		INSERT INTO sessions(id, user_id, expires_at, ip_addr, user_agent)
		VALUES($1, $2, $3, $4, $5)`,
		sessionID, username, expiresAt, ipAddr, userAgent,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	claims := jwt.MapClaims{
		"sub": username,
		"sid": sessionID.String(),
		"exp": expiresAt.Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(b.jwtKey)
}

func (b *Bootstrap) ValidateToken(tokenStr string) (userID string, err error) {
	t, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return b.jwtKey, nil
	})
	if err != nil || !t.Valid {
		return "", errors.New("invalid token")
	}

	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", errors.New("missing sub claim")
	}

	// Check session not revoked
	sid, _ := claims["sid"].(string)
	if sid != "" {
		var revoked bool
		err := b.db.QueryRow(context.Background(),
			"SELECT revoked FROM sessions WHERE id=$1 AND expires_at > NOW()", sid,
		).Scan(&revoked)
		if err != nil || revoked {
			return "", errors.New("session expired or revoked")
		}
	}

	return sub, nil
}

func (b *Bootstrap) RevokeSession(ctx context.Context, tokenStr string) error {
	t, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return b.jwtKey, nil
	})
	if err != nil {
		return nil // best-effort
	}
	claims, _ := t.Claims.(jwt.MapClaims)
	sid, _ := claims["sid"].(string)
	if sid == "" {
		return nil
	}
	_, err = b.db.Exec(ctx, "UPDATE sessions SET revoked=TRUE WHERE id=$1", sid)
	return err
}

func generatePassword() string {
	return uuid.New().String()[:16]
}
