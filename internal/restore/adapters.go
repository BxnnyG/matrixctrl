package restore

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"

	"github.com/bxnnyg/matrixctrl/internal/backup"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// The real ends of the two ports. Everything interesting is in restore.go; these are
// the adapters that let it be tested without a cluster and run against one.

// KubeCluster is the Cluster port on the real client, pinned to one namespace.
type KubeCluster struct {
	K8s       *k8s.Client
	Namespace string
}

func (k KubeCluster) Replicas(ctx context.Context, kind, name string) (int32, error) {
	return k.K8s.Replicas(ctx, k.Namespace, kind, name)
}

func (k KubeCluster) Scale(ctx context.Context, kind, name string, n int32) error {
	return k.K8s.Scale(ctx, k.Namespace, kind, name, n)
}

func (k KubeCluster) WaitGone(ctx context.Context, selector string) error {
	return k.K8s.WaitGone(ctx, k.Namespace, selector)
}

func (k KubeCluster) WaitReady(ctx context.Context, kind, name string) error {
	return k.K8s.WaitReady(ctx, k.Namespace, kind, name)
}

// AdminPG is the Postgres port over a superuser connection.
//
// Superuser is not a convenience here, it is the requirement: `CREATE DATABASE`,
// `ALTER DATABASE … RENAME` and `session_replication_role = replica` are all refused to
// synapse_user, which holds neither `createdb` nor `super` (measured, §4.106). The
// password is read from the cluster secret per restore and lives no longer than the
// operation — the same rule synapseDSN already follows.
type AdminPG struct {
	// DSN points at the server's own `postgres` database: renaming a database cannot be
	// done from a connection to it.
	DSN string

	conn *pgx.Conn
}

// OpenAdmin connects and verifies the connection is actually able to do the job, rather
// than finding out halfway through a restore.
func OpenAdmin(ctx context.Context, dsn string) (*AdminPG, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		// Never echoed: a connection error carries the DSN and the DSN carries the
		// password (§4.70).
		return nil, fmt.Errorf("die Datenbank ist mit dem Administrator-Zugang nicht erreichbar")
	}
	var super bool
	if err := conn.QueryRow(ctx,
		`SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	if !super {
		conn.Close(ctx)
		return nil, fmt.Errorf("dieser Zugang darf keine Datenbanken anlegen oder umbenennen — " +
			"eine Wiederherstellung braucht den Administrator-Zugang aus ess-generated")
	}
	return &AdminPG{DSN: dsn, conn: conn}, nil
}

func (a *AdminPG) Close(ctx context.Context) {
	if a.conn != nil {
		_ = a.conn.Close(ctx)
	}
}

func (a *AdminPG) Exists(ctx context.Context, name string) (bool, error) {
	return backup.DatabaseExists(ctx, a.conn, name)
}

func (a *AdminPG) Create(ctx context.Context, name, owner string) error {
	return backup.CreateDatabase(ctx, a.conn, name, owner)
}

func (a *AdminPG) Rename(ctx context.Context, from, to string) error {
	return backup.RenameDatabase(ctx, a.conn, from, to)
}

// Drop only ever removes a database this restore created minutes earlier and failed to
// fill. The one an operator had before is renamed, never dropped.
func (a *AdminPG) Drop(ctx context.Context, name string) error {
	_, err := a.conn.Exec(ctx, `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize())
	return err
}

func (a *AdminPG) Terminate(ctx context.Context, name string) error {
	return backup.TerminateConnections(ctx, a.conn, name)
}

func (a *AdminPG) Open(ctx context.Context, database string) (Sink, error) {
	u, err := url.Parse(a.DSN)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + database
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		// The error is deliberately not wrapped: a pgx connection failure carries the
		// DSN, and the DSN carries the password (§4.70).
		return nil, fmt.Errorf("Verbindung zur Datenbank %s abgelehnt", database)
	}
	return &loaderSink{conn: conn, Loader: backup.NewLoader(conn)}, nil
}

type loaderSink struct {
	*backup.Loader
	conn *pgx.Conn
}

func (s *loaderSink) Close(ctx context.Context) { _ = s.conn.Close(ctx) }
