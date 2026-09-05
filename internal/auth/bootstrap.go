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

// randomKey returns 32 bytes of entropy, or does not return.
//
// It used to fall back to a time-seeded string. That was reachable from **two**
// places, and the second one mattered: getOrCreateJWTSecret base64-encodes this
// value and INSERTs it as the instance's permanent JWT secret. A failed
// crypto/rand at first boot therefore persisted `matrixctrl-fallback-<unix-nanos>`
// as the signing key — derivable by anyone who can see roughly when the pod
// started, which Kubernetes events publish, and surviving every restart after
// (P1-16).
//
// There is no degraded mode worth having: the alternative to not starting is an
// admin panel with forgeable sessions, which is worse than an admin panel that is
// down and says why.
func randomKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("FATAL: crypto/rand is unavailable (%v) — refusing to start rather than sign sessions with a guessable key", err)
	}
	return b
}

// EnsureAdminExists makes sure there is a way in, on every start and not only the first.
//
// It used to run once, log a generated password, and never speak again. Every part of
// that was a trap (etappe 75):
//
//   - the password existed for one pod lifetime, in one log line, with no way to ask for
//     it again and no way to reset it;
//   - `helm uninstall` leaves the PVC, so reinstalling — the one thing anybody tries —
//     found the old row, did nothing, and said nothing;
//   - MATRIXCTRL_ADMIN_PASSWORD was read only when creating the row, so the documented
//     way to choose a password silently did not apply to any install that had one.
//
// So the environment is now authoritative every time. The chart always puts a password in
// its Secret, which makes that the single place the credential lives, retrievable with
// kubectl for as long as the install exists — and it means an install already locked out
// repairs itself on the next upgrade instead of needing psql.
func (b *Bootstrap) EnsureAdminExists(ctx context.Context) error {
	var exists bool
	err := b.db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM bootstrap_credentials WHERE user_id=$1)", bootstrapUserID,
	).Scan(&exists)
	if err != nil {
		// Not swallowed. This used to return nil on any error, so a database that could
		// not answer produced exactly the same silence as a healthy one that had nothing
		// to do — which is how a locked-out install still looks fine in the log.
		return fmt.Errorf("check for bootstrap admin: %w", err)
	}

	configured := os.Getenv("MATRIXCTRL_ADMIN_PASSWORD")

	switch {
	case configured != "":
		// Authoritative: bring the stored hash in line with the environment on every
		// start. Never logged — it is in the Secret, and a log line only widens the blast
		// radius of the same credential.
		hash, err := bcrypt.GenerateFromPassword([]byte(configured), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		if _, err := b.db.Exec(ctx, `
			INSERT INTO bootstrap_credentials(user_id, password_hash) VALUES($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash`,
			bootstrapUserID, string(hash),
		); err != nil {
			return err
		}
		if exists {
			log.Printf("MatrixCtrl: bootstrap admin password set from MATRIXCTRL_ADMIN_PASSWORD")
		} else {
			log.Printf("MatrixCtrl: bootstrap admin created, password from MATRIXCTRL_ADMIN_PASSWORD")
		}
		return nil

	case exists:
		// The case that produced the original report: nothing to do, and previously
		// nothing said. An operator who cannot get in needs to be told where to look,
		// here, in the log they are already reading.
		log.Printf("MatrixCtrl: bootstrap admin already exists; its password is not recoverable from here")
		log.Printf("MatrixCtrl: to set one: helm upgrade --set secrets.adminPassword=<new> (applied on every start)")
		return nil

	default:
		// No configured password and no admin yet. Generating one keeps a bare
		// `docker run` working, but it is the fallback now, not the design.
		password := generatePassword()
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		if _, err := b.db.Exec(ctx,
			"INSERT INTO bootstrap_credentials(user_id, password_hash) VALUES($1, $2)",
			bootstrapUserID, string(hash),
		); err != nil {
			return err
		}
		log.Printf("MatrixCtrl: bootstrap admin password: %s", password)
		log.Printf("MatrixCtrl: this line is the only copy — set secrets.adminPassword to keep it in the Secret instead")
		return nil
	}
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
	// Same signing-method check as ValidateToken. Not exploitable today — the
	// library refuses "none" without an opt-in and an HMAC byte key is not a usable
	// RSA key — but two validators in one file disagreeing about what they accept is
	// exactly what makes a library upgrade dangerous (P2-28).
	t, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
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
