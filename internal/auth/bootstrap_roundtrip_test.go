package auth

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/db"
)

// TestBootstrapPasswordIsAppliedOnEveryStart is the regression test for an install nobody
// could log into (etappe 75).
//
// MATRIXCTRL_ADMIN_PASSWORD used to be read only when the admin row was *created*. Since
// `helm uninstall` leaves the database volume behind, a reinstall found the old row, did
// nothing, printed nothing, and ignored the password the operator had just set — the one
// documented way in did not apply to the only situation where anybody needs it.
//
// Skipped without a DSN so `go test ./...` stays hermetic. It creates and drops its own
// database and never writes to the one in the DSN:
//
//	MATRIXCTRL_BACKUP_TEST_DSN=postgres://…/matrixctrl go test ./internal/auth/
func TestBootstrapPasswordIsAppliedOnEveryStart(t *testing.T) {
	dsn := os.Getenv("MATRIXCTRL_BACKUP_TEST_DSN")
	if dsn == "" {
		t.Skip("set MATRIXCTRL_BACKUP_TEST_DSN to run against a real database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	scratch := fmt.Sprintf("matrixctrl_auth_%d", os.Getpid())
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+scratch); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+scratch); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`)
		admin.Close()
	}()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + scratch
	pool, err := db.New(ctx, u.String())
	if err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	defer pool.Close()

	b := NewBootstrap(ctx, pool)

	// First start: the operator set a password at install time.
	t.Setenv("MATRIXCTRL_ADMIN_PASSWORD", "first-password")
	if err := b.EnsureAdminExists(ctx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := b.Login(ctx, "admin", "first-password", "127.0.0.1", "test"); err != nil {
		t.Fatalf("cannot log in after install: %v", err)
	}

	// Restart with the same password: still works, and no duplicate row.
	if err := b.EnsureAdminExists(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if _, err := b.Login(ctx, "admin", "first-password", "127.0.0.1", "test"); err != nil {
		t.Fatalf("cannot log in after restart: %v", err)
	}

	// The case that was broken: the row already exists and the operator sets a new
	// password. Before etappe 75 this changed nothing and the old password stayed.
	t.Setenv("MATRIXCTRL_ADMIN_PASSWORD", "second-password")
	if err := b.EnsureAdminExists(ctx); err != nil {
		t.Fatalf("password change: %v", err)
	}
	if _, err := b.Login(ctx, "admin", "second-password", "127.0.0.1", "test"); err != nil {
		t.Fatalf("the new password was ignored — this is the bug that locked the operator out: %v", err)
	}
	if _, err := b.Login(ctx, "admin", "first-password", "127.0.0.1", "test"); err == nil {
		t.Error("the old password still works after a reset")
	}

	// And with no password configured, an existing admin is left alone rather than
	// silently replaced by a generated one nobody would ever see.
	os.Unsetenv("MATRIXCTRL_ADMIN_PASSWORD")
	if err := b.EnsureAdminExists(ctx); err != nil {
		t.Fatalf("start without a configured password: %v", err)
	}
	if _, err := b.Login(ctx, "admin", "second-password", "127.0.0.1", "test"); err != nil {
		t.Errorf("an existing password must survive a start with no password configured: %v", err)
	}
}
