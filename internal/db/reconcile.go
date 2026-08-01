package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// nonTerminalUpgradeStates are the states an upgrade passes through while its
// goroutine is alive. Nothing else ever writes them, so a row still sitting in
// one of these at startup describes an upgrade whose process is gone.
var nonTerminalUpgradeStates = []string{"pending", "running", "running-hooks"}

// interruptedMessage is what the operator sees instead of a status that never
// resolves. It says what is known and what is not, because "unknown" is the
// honest answer here: the Helm revision may well have gone through, while
// whether the post-upgrade hooks ran cannot be recovered after the fact.
const interruptedMessage = "MatrixCtrl restarted while this upgrade was running, so its outcome was never recorded. " +
	"Check the release revision and re-run the post-upgrade hooks if in doubt."

// ReconcileInterruptedUpgrades closes out upgrades that no process is running
// any more. Call it once at startup, before serving.
//
// An upgrade's terminal status is written by the goroutine that drives it. If
// that process dies in between — a pod restart, an OOM kill, a node blip — the
// row keeps its in-flight status forever and nothing ever revisits it. The
// production instance had exactly one such row: `running-hooks`, helm_revision
// 22, no completion timestamp, while the release itself was revision 22
// `deployed` and both hooks reported OK. The upgrade had succeeded a day
// earlier; only the record was stuck (BACKLOG P2-16).
//
// This is deliberately not a heartbeat or a lease. Startup is the one moment
// where "no process owns this row" is known for certain, because the process
// that could have owned it is the one starting up.
func ReconcileInterruptedUpgrades(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE upgrade_history
		   SET status = 'interrupted',
		       ts_completed = NOW(),
		       error_message = COALESCE(NULLIF(error_message, ''), $1)
		 WHERE status = ANY($2)`,
		interruptedMessage, nonTerminalUpgradeStates,
	)
	if err != nil {
		return 0, fmt.Errorf("reconcile interrupted upgrades: %w", err)
	}
	return tag.RowsAffected(), nil
}
