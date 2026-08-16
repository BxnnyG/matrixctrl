package helm

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

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
	// revCache holds the immutable per-revision facts the history page needs,
	// keyed release → revision. Guarded by relMu (etappe 39).
	revCache map[string]map[int]revisionFacts

	// facts persists what revCache holds, so the cold read happens once per
	// revision rather than once per process. Nil is supported and means
	// memory-only: the page is then as fast as E39 left it and no slower
	// (etappe 42).
	facts RevisionStore
}

// RevisionStore persists per-revision facts across restarts.
//
// An interface rather than a *pgxpool.Pool so this package keeps not knowing about
// the database — internal/helm talks to Helm and Kubernetes, and the one thing it
// now wants to remember is passed in by whoever owns the connection.
type RevisionStore interface {
	LoadRevisionFacts(ctx context.Context, release string) (map[int]RevisionFact, error)
	SaveRevisionFacts(ctx context.Context, release string, facts map[int]RevisionFact) error
}

// RevisionFact is the exported shape of what gets stored. Deliberately not the
// unexported revisionFacts: a store implemented elsewhere should not have to reach
// into this package's internals.
type RevisionFact struct {
	Chart      string
	DeployedAt time.Time
}

// SetRevisionStore attaches persistence. Called once at startup, before serving.
func (c *Client) SetRevisionStore(s RevisionStore) { c.facts = s }

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
