package helm

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
)

// SelfChartOCI is where MatrixCtrl's own chart is published.
const SelfChartOCI = "oci://ghcr.io/bxnnyg/charts/matrixctrl"

// UpgradeSelf upgrades MatrixCtrl's own release to version (etappe 116).
//
// Runs in a Job, never in the panel's pod: the Deployment is `Recreate`, so the pod that
// asked for the upgrade is the first thing the upgrade removes. Atomic, so a version that
// does not become ready within the timeout is rolled back by the same process that
// applied it — nobody else would be there to do it.
//
// values are the release's own, prepared by the caller (selfupdate.CarryValues).
func (c *Client) UpgradeSelf(ctx context.Context, release, version string, values map[string]interface{}, log func(string, ...interface{})) error {
	ch, cleanup, err := c.pullChartFrom(SelfChartOCI, version)
	if err != nil {
		return err
	}
	defer cleanup()

	up := action.NewUpgrade(c.cfg)
	up.Namespace = c.namespace
	up.Wait = true
	up.Atomic = true // rolls back on failure; implies Wait
	up.Timeout = 5 * time.Minute
	up.CleanupOnFail = true
	if log != nil {
		log("Chart %s@%s geladen, starte Upgrade (warten bis bereit, sonst Rücksprung)…", SelfChartOCI, version)
	}
	defer c.InvalidateRelease(release)
	rel, err := up.RunWithContext(ctx, release, ch, values)
	if err != nil {
		return fmt.Errorf("upgrade %s auf %s: %w", release, version, err)
	}
	if log != nil {
		log("Revision %d ist %s.", rel.Version, rel.Info.Status)
	}
	return nil
}

// ReleaseUserValues are the values the operator supplied to a release — what `helm get
// values` prints, without the chart's defaults.
func (c *Client) ReleaseUserValues(release string) (map[string]interface{}, error) {
	get := action.NewGetValues(c.cfg)
	get.AllValues = false
	return get.Run(release)
}
