package notify

import (
	"context"
	"errors"

	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/bxnnyg/matrixctrl/internal/helm"
)

// HelmReleases reads the release through internal/helm (no shelling out). The release
// name is the one main.go resolved, including auto-discovery in another namespace.
type HelmReleases struct {
	Client *helm.Client
	Name   string
}

func (h HelmReleases) Release(ctx context.Context) (*ReleaseState, error) {
	if h.Client == nil {
		return nil, nil
	}
	type out struct {
		info *helm.ReleaseInfo
		err  error
	}
	ch := make(chan out, 1)
	go func() {
		i, e := h.Client.GetRelease(h.Name)
		ch <- out{i, e}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case o := <-ch:
		if errors.Is(o.err, driver.ErrReleaseNotFound) {
			return nil, nil
		}
		if o.err != nil {
			return nil, o.err
		}
		if o.info == nil {
			return nil, nil
		}
		return &ReleaseState{Name: o.info.Name, Status: o.info.Status, Since: o.info.DeployedAt}, nil
	}
}
