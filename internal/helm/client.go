package helm

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type Client struct {
	cfg       *action.Configuration
	namespace string

	// Cache for GetRelease — see cache.go for why it exists.
	relMu    sync.Mutex
	relCache map[string]cachedRelease
	now      func() time.Time // nil means time.Now; set in tests
}

func New(namespace string) (*Client, error) {
	flags := genericclioptions.NewConfigFlags(true)
	flags.Namespace = &namespace

	// Use KUBECONFIG env or in-cluster (ConfigFlags handles this automatically)
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		flags.KubeConfig = &kc
	}

	cfg := new(action.Configuration)
	if err := cfg.Init(flags, namespace, "secret", log.Printf); err != nil {
		return nil, fmt.Errorf("helm init: %w", err)
	}

	return &Client{cfg: cfg, namespace: namespace}, nil
}
