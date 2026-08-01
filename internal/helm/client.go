package helm

import (
	"fmt"
	"log"
	"os"
	"sync"

	"helm.sh/helm/v3/pkg/action"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/metadata"
)

type Client struct {
	cfg       *action.Configuration
	namespace string

	// meta reads object metadata without object payloads — see release_read.go.
	// Nil is a supported state: every path that uses it falls back to the plain
	// Helm read, so a cluster that refuses the metadata API is slow, not broken.
	meta metadata.Interface

	// Memoised release info — see cache.go for what keys it.
	relMu    sync.Mutex
	relCache map[string]memoisedRelease
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

	c := &Client{cfg: cfg, namespace: namespace}

	// A missing metadata client costs speed, not correctness, so it must not turn
	// an otherwise working Helm client into a startup failure.
	if restCfg, err := flags.ToRESTConfig(); err != nil {
		log.Printf("helm: metadata client unavailable (%v) — release reads will use the slow path", err)
	} else if mc, err := metadata.NewForConfig(restCfg); err != nil {
		log.Printf("helm: metadata client unavailable (%v) — release reads will use the slow path", err)
	} else {
		c.meta = mc
	}

	return c, nil
}
