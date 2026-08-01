package db

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	// Retry the initial connection — the Postgres sidecar starts in parallel in
	// the same pod.
	//
	// 60s used to be the limit, and it was not enough for a *first* start: an
	// empty PVC means Postgres has to run initdb before it accepts connections,
	// which took longer than that on a fresh cluster. So the first container of
	// every new install exited 1 and was restarted by Kubernetes — it recovered,
	// but a brand-new install greeted its operator with restarts=1, from a tool
	// whose own dashboard flags restarts as a problem. Found in etappe 15; an
	// existing install never hits it because the database is already initialised.
	//
	// Five minutes is generous on purpose: the cost of waiting is a slower start,
	// the cost of giving up early is a crash loop that looks like a broken install.
	// Kubernetes restarting the container remains the backstop if Postgres really
	// never comes up.
	const maxWait = 5 * time.Minute
	deadline := time.Now().Add(maxWait)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("ping (after %s): %w", maxWait, err)
		}
		log.Printf("database not ready yet, retrying: %v", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if err := migrate(ctx, pool); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return pool, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		var applied bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", name,
		).Scan(&applied)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}

		if _, err := pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO schema_migrations(version) VALUES($1)", name,
		); err != nil {
			return err
		}
	}
	return nil
}
