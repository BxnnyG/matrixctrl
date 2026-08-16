package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/helm"
)

// RevisionFacts persists the per-revision Helm facts the upgrade-history page needs
// (etappe 42).
//
// It lives here rather than in internal/helm because that package's rule is that it
// talks to Helm and Kubernetes and nothing else. The seam is helm.RevisionStore, and
// this is the one implementation.
//
// What is stored is immutable by construction: a revision's chart version and
// deployment time are fixed when Helm writes it, and a rollback appends a new
// highest revision rather than editing an old one. So there is no invalidation
// here, and none is missing.
type RevisionFacts struct {
	pool *pgxpool.Pool
}

func NewRevisionFacts(pool *pgxpool.Pool) *RevisionFacts { return &RevisionFacts{pool: pool} }

func (r *RevisionFacts) LoadRevisionFacts(ctx context.Context, release string) (map[int]helm.RevisionFact, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT revision, chart, deployed_at FROM helm_revision_facts WHERE release_name = $1`, release)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]helm.RevisionFact{}
	for rows.Next() {
		var rev int
		var chart string
		var deployedAt *time.Time
		if err := rows.Scan(&rev, &chart, &deployedAt); err != nil {
			return nil, err
		}
		f := helm.RevisionFact{Chart: chart}
		if deployedAt != nil {
			f.DeployedAt = *deployedAt
		}
		out[rev] = f
	}
	return out, rows.Err()
}

func (r *RevisionFacts) SaveRevisionFacts(ctx context.Context, release string, facts map[int]helm.RevisionFact) error {
	batch := &pgx.Batch{}
	for rev, f := range facts {
		var deployedAt *time.Time
		if !f.DeployedAt.IsZero() {
			deployedAt = &f.DeployedAt
		}
		// DO NOTHING rather than DO UPDATE: a row that already exists describes the
		// same immutable revision, so rewriting it could only ever replace a correct
		// value with an equal one — or, if the two ever disagreed, would silently
		// prefer the newer read over the one already proven to render correctly.
		batch.Queue(`
			INSERT INTO helm_revision_facts(release_name, revision, chart, deployed_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (release_name, revision) DO NOTHING`,
			release, rev, f.Chart, deployedAt)
	}
	return r.pool.SendBatch(ctx, batch).Close()
}
