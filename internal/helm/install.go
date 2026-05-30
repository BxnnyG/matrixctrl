package helm

import (
	"context"
	"fmt"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmcli "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

// pullChart pulls + unpacks the ESS chart at the given version into a temp dir and
// loads it. The returned cleanup removes the temp dir.
func (c *Client) pullChart(version string) (*chart.Chart, func(), error) {
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, nil, fmt.Errorf("registry client: %w", err)
	}
	c.cfg.RegistryClient = regClient

	destDir, err := os.MkdirTemp("", "matrixctrl-chart-")
	if err != nil {
		return nil, nil, fmt.Errorf("temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(destDir) }

	pull := action.NewPullWithOpts(action.WithConfig(c.cfg))
	pull.Settings = helmcli.New()
	pull.Version = version
	pull.DestDir = destDir
	pull.Untar = true
	if _, err := pull.Run(essChartOCI); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("pull chart %s@%s: %w", essChartOCI, version, err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil || len(entries) == 0 {
		cleanup()
		return nil, nil, fmt.Errorf("chart directory empty after pull")
	}
	ch, err := loader.Load(destDir + "/" + entries[0].Name())
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("load chart: %w", err)
	}
	return ch, cleanup, nil
}

// DefaultChartValues returns the raw, comment-rich values.yaml shipped with the
// ESS chart at the given version. This is the seed source for a greenfield config
// (Phase 1.5) — the operator starts from the documented defaults and edits down.
func (c *Client) DefaultChartValues(version string) (string, error) {
	ch, cleanup, err := c.pullChart(version)
	if err != nil {
		return "", err
	}
	defer cleanup()
	for _, f := range ch.Raw {
		if f.Name == "values.yaml" {
			return string(f.Data), nil
		}
	}
	return "", fmt.Errorf("chart %s has no values.yaml", version)
}

// Install installs a fresh ESS release (greenfield). Fails if the release already
// exists — use Upgrade for existing releases.
func (c *Client) Install(ctx context.Context, releaseName, version string, values map[string]interface{}) (*UpgradeResult, error) {
	ch, cleanup, err := c.pullChart(version)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	install := action.NewInstall(c.cfg)
	install.ReleaseName = releaseName
	install.Namespace = c.namespace
	install.CreateNamespace = true
	install.Wait = true
	install.Timeout = 15 * time.Minute

	if values == nil {
		values = map[string]interface{}{}
	}
	rel, err := install.RunWithContext(ctx, ch, values)
	if err != nil {
		return nil, fmt.Errorf("helm install: %w", err)
	}
	return &UpgradeResult{Revision: rel.Version, Status: rel.Info.Status.String()}, nil
}
