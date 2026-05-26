package helm

import (
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
)

type ReleaseInfo struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	ChartVersion string    `json:"chart_version"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	DeployedAt   time.Time `json:"deployed_at,omitempty"`
}

type RevisionEntry struct {
	Revision   int       `json:"revision"`
	Status     string    `json:"status"`
	Chart      string    `json:"chart"`
	DeployedAt time.Time `json:"deployed_at"`
	Notes      string    `json:"notes,omitempty"`
}

func (c *Client) GetRelease(name string) (*ReleaseInfo, error) {
	get := action.NewGet(c.cfg)
	rel, err := get.Run(name)
	if err != nil {
		return nil, fmt.Errorf("get release %s: %w", name, err)
	}
	return toReleaseInfo(rel), nil
}

func (c *Client) ListHistory(name string, max int) ([]RevisionEntry, error) {
	hist := action.NewHistory(c.cfg)
	hist.Max = max
	releases, err := hist.Run(name)
	if err != nil {
		return nil, fmt.Errorf("history %s: %w", name, err)
	}

	entries := make([]RevisionEntry, 0, len(releases))
	for _, r := range releases {
		entries = append(entries, RevisionEntry{
			Revision:   r.Version,
			Status:     r.Info.Status.String(),
			Chart:      r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version,
			DeployedAt: r.Info.LastDeployed.Time,
		})
	}
	return entries, nil
}

func toReleaseInfo(r *release.Release) *ReleaseInfo {
	info := &ReleaseInfo{
		Name:         r.Name,
		Namespace:    r.Namespace,
		ChartVersion: r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version,
		Revision:     r.Version,
		Status:       r.Info.Status.String(),
	}
	if r.Info != nil {
		info.DeployedAt = r.Info.LastDeployed.Time
	}
	return info
}
