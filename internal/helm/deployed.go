package helm

import (
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/release"
)

// Rendering a configuration against the chart that is actually deployed (etappe 108).
//
// Render (E55) pulls the chart from the registry for every question: seconds per call,
// and no answer at all without outbound internet (edge case 5). The release already
// carries its complete chart — the same insight that gave the settings form its schema
// in etappe 107 — so "what would this configuration become?" is answered from there.

// newestRelease decodes the newest revision of a release: one secret, ~300 ms, found
// through the same cheap probe the release view uses.
func (c *Client) newestRelease(name string) (*release.Release, releaseIdentity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	id, err := c.probeNewestRelease(ctx, name)
	if err != nil {
		return nil, releaseIdentity{}, fmt.Errorf("release %s: %w", name, err)
	}
	rel, err := c.cfg.Releases.Get(name, id.Revision)
	if err != nil {
		return nil, releaseIdentity{}, fmt.Errorf("release %s revision %d: %w", name, id.Revision, err)
	}
	return rel, id, nil
}

// RenderDeployed returns the manifest that is running now and the one these values would
// produce with the same chart — the two inputs for "what would change".
//
// A template render, as `helm template` does it, and not an upgrade dry run. Both were
// measured against the live release: the upgrade dry run took ~30 s whether client- or
// server-side, because it reads the whole release history and resolves every object
// through API discovery (which on this cluster waits on a broken metrics.k8s.io); the
// template render took 2 s. Both predicted the same restarts — none for the running
// values, exactly Synapse for a changed Synapse memory. So the preview renders, and the
// server-side preflight inside the real upgrade stays the gate that decides (§4.83).
//
// Two settings make the render match what an upgrade would produce: the cluster's real
// Kubernetes version (the default is a fake v1.20, which ESS rejects), and IsUpgrade, so
// templates branching on .Release.IsUpgrade take the upgrade path.
func (c *Client) RenderDeployed(ctx context.Context, name string, values map[string]interface{}) (current, rendered string, err error) {
	rel, _, err := c.newestRelease(name)
	if err != nil {
		return "", "", err
	}
	if rel.Chart == nil {
		return "", "", fmt.Errorf("release %s carries no chart", name)
	}

	kc, err := c.cfg.KubernetesClientSet()
	if err != nil {
		return rel.Manifest, "", fmt.Errorf("cluster version: %w", err)
	}
	sv, err := kc.Discovery().ServerVersion()
	if err != nil {
		return rel.Manifest, "", fmt.Errorf("cluster version: %w", err)
	}
	kv, err := chartutil.ParseKubeVersion(sv.GitVersion)
	if err != nil {
		return rel.Manifest, "", fmt.Errorf("cluster version %q: %w", sv.GitVersion, err)
	}

	// A configuration of its own, never c.cfg. ClientOnly is how `helm template` works,
	// and it does so by *replacing* fields of the configuration it is handed: Releases
	// becomes an empty in-memory store and KubeClient a fake that prints to io.Discard.
	// On the shared configuration that turned the whole client into a stub — the second
	// call of the live test could no longer find the release it had just read, and a real
	// deploy after any preview would have "applied" to nowhere and reported success. The
	// test caught it only because it renders twice (etappe 108).
	isolated := &action.Configuration{Log: func(string, ...interface{}) {}}
	tmpl := action.NewInstall(isolated)
	tmpl.DryRun, tmpl.ClientOnly, tmpl.Replace = true, true, true
	// Nothing is applied: a hook run by a "what would happen" question is a hook that
	// has already changed something.
	tmpl.DisableHooks = true
	tmpl.IsUpgrade = true
	tmpl.KubeVersion = kv
	tmpl.ReleaseName, tmpl.Namespace = name, c.namespace
	if values == nil {
		values = map[string]interface{}{}
	}
	out, err := tmpl.RunWithContext(ctx, rel.Chart, values)
	if err != nil {
		return rel.Manifest, "", fmt.Errorf("render: %w", err)
	}
	return rel.Manifest, out.Manifest, nil
}
