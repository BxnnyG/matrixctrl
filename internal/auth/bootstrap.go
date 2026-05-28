package auth

import (
	"context"
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

func NewBootstrap(db *pgxpool.Pool) *Bootstrap {
	key := []byte(os.Getenv("MATRIXCTRL_JWT_SECRET"))
	if len(key) == 0 {
		// Derive a fixed key for dev; in production set MATRIXCTRL_JWT_SECRET
		key = []byte("matrixctrl-dev-secret-change-in-prod")
	}
	return &Bootstrap{db: db, jwtKey: key}
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
