package helm

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
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
