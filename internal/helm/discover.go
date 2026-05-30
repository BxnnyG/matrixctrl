package helm

import (
	"fmt"
	"log"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// ESSRelease is a discovered ESS (matrix-stack) Helm release.
type ESSRelease struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
}

// Discover scans every namespace for Helm releases of the ESS `matrix-stack`
// chart, so MatrixCtrl can adopt an existing ESS without the operator hard-coding
// the namespace/release. Read-only.
func Discover() ([]ESSRelease, error) {
	flags := genericclioptions.NewConfigFlags(true)
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		flags.KubeConfig = &kc
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(flags, "", "secret", log.Printf); err != nil {
		return nil, fmt.Errorf("helm init: %w", err)
	}
	list := action.NewList(cfg)
	list.AllNamespaces = true
	list.All = true
	releases, err := list.Run()
	if err != nil {
		return nil, err
	}
	var out []ESSRelease
	for _, r := range releases {
		if r.Chart == nil || r.Chart.Metadata == nil || r.Info == nil {
			continue
		}
		if r.Chart.Metadata.Name == "matrix-stack" {
			out = append(out, ESSRelease{
				Namespace: r.Namespace,
				Name:      r.Name,
				Version:   r.Chart.Metadata.Version,
				Status:    r.Info.Status.String(),
			})
		}
	}
	return out, nil
}

// GetReleaseValues returns the user-supplied (override) values of a deployed
// release — the equivalent of `helm get values`, used to adopt an existing ESS
// into the config repo so future upgrades carry the same overrides.
func (c *Client) GetReleaseValues(name string) (map[string]interface{}, error) {
	get := action.NewGetValues(c.cfg)
	get.AllValues = false
	return get.Run(name)
}
