package helm

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
)

const essChartOCI = "oci://ghcr.io/element-hq/ess-helm/matrix-stack"

type UpgradeResult struct {
	Revision int
	Status   string
}

func (c *Client) Upgrade(ctx context.Context, releaseName, toVersion string, values map[string]interface{}) (*UpgradeResult, error) {
	chart, cleanup, err := c.pullChart(toVersion)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	upgrade := action.NewUpgrade(c.cfg)
	upgrade.Namespace = c.namespace
	upgrade.Wait = true
	upgrade.Timeout = 10 * time.Minute

	if values == nil {
		values = map[string]interface{}{}
	}

	// Ask first whether this can work at all.
	//
	// An upgrade that is going to be refused is refused after the rollout has started
	// and the operator has watched a progress line for three minutes — and the reason
	// was knowable in a second. Worse, some refusals arrive after the release has
	// already moved, which is how an install ends up in pending-upgrade and blocks
	// every later command (§4.88).
	//
	// A server-side dry run renders against the live cluster: `lookup` works, ownership
	// conflicts surface, and the schema is validated. Nothing is applied.
	if err := c.preflight(ctx, releaseName, chart, values); err != nil {
		return nil, err
	}

	// Same reasoning as Rollback: a failed upgrade may still have moved the release.
	defer c.InvalidateRelease(releaseName)

	rel, err := upgrade.RunWithContext(ctx, releaseName, chart, values)
	if err != nil {
		return nil, fmt.Errorf("helm upgrade: %w", err)
	}

	return &UpgradeResult{
		Revision: rel.Version,
		Status:   rel.Info.Status.String(),
	}, nil
}

// preflight renders the upgrade against the live cluster without applying it.
//
// Deliberately server-side (DryRunOption "server"). A client-side dry run makes no API
// calls, so it cannot see that an object already exists without Helm ownership — and it
// renders templates guarded by `lookup` as if the cluster were empty, which produces
// failures that a real install would not have and misses the ones it would (§4.83).
func (c *Client) preflight(ctx context.Context, releaseName string, chart *chart.Chart, values map[string]interface{}) error {
	dry := action.NewUpgrade(c.cfg)
	dry.Namespace = c.namespace
	dry.DryRun = true
	dry.DryRunOption = "server"
	// Nothing is applied, so there is nothing to wait for — and a hook that runs during
	// a "would this work" question has already changed something.
	dry.Wait = false
	dry.DisableHooks = true

	if _, err := dry.RunWithContext(ctx, releaseName, chart, values); err != nil {
		return fmt.Errorf("preflight: this upgrade would fail, so nothing was changed: %w", err)
	}
	return nil
}

func (c *Client) Rollback(releaseName string, revision int) error {
	rollback := action.NewRollback(c.cfg)
	rollback.Version = revision
	rollback.Wait = true
	rollback.Timeout = 5 * time.Minute
	// Invalidate unconditionally: a rollback that fails part-way through can still
	// have changed the release, so trusting the error to mean "nothing happened"
	// would leave a stale entry behind.
	defer c.InvalidateRelease(releaseName)
	return rollback.Run(releaseName)
}

// Render returns the manifest an upgrade *would* produce, without touching the
// cluster (etappe 55).
//
// A dry run rather than a values-file inspection, because the questions worth asking
// about a config are answered by the chart and not by the values: which containers
// share a `resources` block, which init containers inherit it, how many pods a value
// is multiplied across. Reading `cpu: 4000m` out of postgres.yaml and believing it
// means 4000m is how a homeserver spent 37 hours unschedulable (§4.53).
func (c *Client) Render(ctx context.Context, releaseName, version string, values map[string]interface{}) (string, error) {
	chart, cleanup, err := c.pullChart(version)
	if err != nil {
		return "", err
	}
	defer cleanup()

	upgrade := action.NewUpgrade(c.cfg)
	upgrade.Namespace = c.namespace
	upgrade.DryRun = true
	// No waiting and no hooks: nothing is being applied, and a hook that runs during a
	// "what would happen" question is a hook that has already changed something.
	upgrade.Wait = false
	upgrade.DisableHooks = true

	if values == nil {
		values = map[string]interface{}{}
	}

	rel, err := upgrade.RunWithContext(ctx, releaseName, chart, values)
	if err != nil {
		return "", fmt.Errorf("render chart: %w", err)
	}
	return rel.Manifest, nil
}
