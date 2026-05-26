package helm

import (
	"context"
	"fmt"
	"os"
	"time"

	helmcli "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
)

const essChartOCI = "oci://ghcr.io/element-hq/ess-helm/matrix-stack"

type UpgradeResult struct {
	Revision int
	Status   string
}

func (c *Client) Upgrade(ctx context.Context, releaseName, toVersion string, values map[string]interface{}) (*UpgradeResult, error) {
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("registry client: %w", err)
	}
	c.cfg.RegistryClient = regClient

	settings := helmcli.New()

	// Pull chart to a temp dir
	destDir, err := os.MkdirTemp("", "matrixctrl-chart-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(destDir)

	pull := action.NewPullWithOpts(action.WithConfig(c.cfg))
	pull.Settings = settings
	pull.Version = toVersion
	pull.DestDir = destDir
	pull.Untar = true

	if _, err := pull.Run(essChartOCI); err != nil {
		return nil, fmt.Errorf("pull chart %s@%s: %w", essChartOCI, toVersion, err)
	}

	// Find the unpacked chart directory
	entries, err := os.ReadDir(destDir)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("chart directory empty after pull")
	}
	chartPath := destDir + "/" + entries[0].Name()

	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}

	upgrade := action.NewUpgrade(c.cfg)
	upgrade.Namespace = c.namespace
	upgrade.Wait = true
	upgrade.Timeout = 10 * time.Minute

	if values == nil {
		values = map[string]interface{}{}
	}

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
	return rollback.Run(releaseName)
}
