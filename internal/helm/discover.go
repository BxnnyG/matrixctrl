package helm

import (
	"fmt"
	"log"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// ESSRelease is a discovered ESS (matrix-stack) Helm release.
type ESSRelease struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
}

// DiscoveryResult separates what was found from how far the search reached.
//
// The two are not the same answer, and conflating them is how "no ESS here" gets
// reported for an ESS that is simply in another namespace. A caller that only reads
// Releases will be right whenever the scan was cluster-wide and quietly wrong when
// it was not — so the scope travels with the result rather than beside it.
type DiscoveryResult struct {
	Releases []ESSRelease `json:"releases"`
	// ClusterWide is false when the cluster refused a cluster-wide secret list and
	// only Namespace was searched.
	//
	// Since etappe 40 that is the **normal** posture rather than an edge case: the
	// role is namespaced, so Helm's cluster-wide release scan — which it implements
	// as listing every Secret in the cluster — is denied, and this fallback is the
	// path that actually runs. It was written in etappe 37 for a permission that did
	// not yet exist to be missing.
	ClusterWide bool `json:"cluster_wide"`
	// Namespace is the single namespace searched when ClusterWide is false.
	Namespace string `json:"searched_namespace,omitempty"`
}

// Discover looks for Helm releases of the ESS `matrix-stack` chart so MatrixCtrl can
// adopt an existing ESS without the operator hard-coding namespace and release.
// Read-only.
//
// It tries the whole cluster first and falls back to fallbackNS when that is refused,
// rather than requiring the caller to know which permission it has. The fallback is
// the normal path, not an error case: a cluster-wide scan needs a permanent read of
// every Secret in the cluster, which the chart stopped granting when the role became
// namespaced (etappe 40).
func Discover(fallbackNS string) (DiscoveryResult, error) {
	releases, err := listReleases("", true)
	if err == nil {
		return DiscoveryResult{Releases: essOnly(releases), ClusterWide: true}, nil
	}

	if !isForbidden(err) || fallbackNS == "" {
		return DiscoveryResult{}, err
	}

	// Refused, as configured. Ask the one namespace we are allowed to ask about.
	releases, nsErr := listReleases(fallbackNS, false)
	if nsErr != nil {
		// Report the *first* failure. The fallback failing too usually means Helm
		// cannot talk to the cluster at all, and the cluster-wide error says that
		// more precisely than "forbidden in one namespace" does.
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{
		Releases:  essOnly(releases),
		Namespace: fallbackNS,
	}, nil
}

func listReleases(namespace string, allNamespaces bool) ([]*release.Release, error) {
	flags := genericclioptions.NewConfigFlags(true)
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		flags.KubeConfig = &kc
	}
	if namespace != "" {
		flags.Namespace = &namespace
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(flags, namespace, "secret", log.Printf); err != nil {
		return nil, fmt.Errorf("helm init: %w", err)
	}
	list := action.NewList(cfg)
	list.AllNamespaces = allNamespaces
	list.All = true
	return list.Run()
}

func essOnly(releases []*release.Release) []ESSRelease {
	// Always a list, never nil: a frontend checking `.length` would otherwise crash
	// on the page whose whole job is to explain what is wrong.
	out := []ESSRelease{}
	for _, r := range releases {
		if r.Chart == nil || r.Chart.Metadata == nil || r.Info == nil {
			continue
		}
		if r.Chart.Metadata.Name != "matrix-stack" {
			continue
		}
		out = append(out, ESSRelease{
			Namespace: r.Namespace,
			Name:      r.Name,
			Version:   r.Chart.Metadata.Version,
			Status:    r.Info.Status.String(),
		})
	}
	return out
}

// isForbidden recognises the one failure that has a useful fallback.
//
// The typed check is the real one. The string check behind it exists because Helm's
// storage driver wraps the API error in its own text on some paths, and a wrapped
// 403 that reads as an unknown failure would turn a supported configuration into a
// dead setup wizard. A false positive costs one extra namespaced list.
func isForbidden(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsForbidden(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forbidden") || strings.Contains(msg, "cannot list resource \"secrets\"")
}

// GetReleaseValues returns the user-supplied (override) values of a deployed
// release — the equivalent of `helm get values`, used to adopt an existing ESS
// into the config repo so future upgrades carry the same overrides.
func (c *Client) GetReleaseValues(name string) (map[string]interface{}, error) {
	get := action.NewGetValues(c.cfg)
	get.AllValues = false
	return get.Run(name)
}
