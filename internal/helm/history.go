package helm

import (
	"fmt"
	"sort"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
)

type ReleaseInfo struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	ChartVersion string    `json:"chart_version"` // e.g. "matrix-stack-26.5.1"
	Version      string    `json:"version"`       // e.g. "26.5.1" (semver only)
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

// GetRelease returns the current state of a release. See release_read.go for how
// it avoids paying to decode ten revisions it is going to throw away.
func (c *Client) GetRelease(name string) (*ReleaseInfo, error) {
	return c.readRelease(name)
}

// getReleaseUncached is the original implementation, kept as the fallback for
// every way the fast path can fail. action.NewGet decodes the entire release
// history to return one revision, which is why it costs ~4.3 s on the production
// release — acceptable as a rare correctness backstop, not as the normal path.
func (c *Client) getReleaseUncached(name string) (*ReleaseInfo, error) {
	get := action.NewGet(c.cfg)
	rel, err := get.Run(name)
	if err != nil {
		return nil, fmt.Errorf("get release %s: %w", name, err)
	}
	return toReleaseInfo(rel), nil
}

// ListHistory returns the newest `max` revisions of a release, newest first.
//
// See history_read.go for why this reads labels first and decodes as little as it
// can: the straightforward call costs 3.2–4.6 s on the production release and is
// paid on every visit to the page an operator opens when something has gone wrong.
func (c *Client) ListHistory(name string, max int) ([]RevisionEntry, error) {
	if entries, err := c.listHistoryFast(name, max); err == nil {
		return entries, nil
	}
	return c.listHistoryUncached(name, max)
}

// listHistoryUncached is the original implementation, kept as the fallback for
// every way the fast path can fail — the same shape as getReleaseUncached, and for
// the same reason: the worst case should be the old latency, not a wrong answer.
func (c *Client) listHistoryUncached(name string, max int) ([]RevisionEntry, error) {
	hist := action.NewHistory(c.cfg)
	// Set for the record, though Helm never reads it: History.Run calls
	// Releases.History(name) and returns everything. Truncation below is ours.
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
	sort.Slice(entries, func(i, j int) bool { return entries[i].Revision > entries[j].Revision })
	if max > 0 && len(entries) > max {
		entries = entries[:max]
	}
	return entries, nil
}

// factsOf extracts the fields that never change again once a revision is written.
func factsOf(r *release.Release) revisionFacts {
	f := revisionFacts{}
	if r.Chart != nil && r.Chart.Metadata != nil {
		f.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
	}
	if r.Info != nil {
		f.DeployedAt = r.Info.LastDeployed.Time
	}
	return f
}

func toReleaseInfo(r *release.Release) *ReleaseInfo {
	info := &ReleaseInfo{
		Name:         r.Name,
		Namespace:    r.Namespace,
		ChartVersion: r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version,
		Version:      r.Chart.Metadata.Version,
		Revision:     r.Version,
		Status:       r.Info.Status.String(),
	}
	if r.Info != nil {
		info.DeployedAt = r.Info.LastDeployed.Time
	}
	return info
}
