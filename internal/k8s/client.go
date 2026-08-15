package k8s

import (
	"fmt"
	"log"
	"os"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Static  *kubernetes.Clientset
	Dynamic dynamic.Interface
	// Meta lists objects without their spec. Field ownership lives in metadata, so
	// the drift check reads it this way rather than pulling whole objects — the
	// same trade E20 made for release reads. May be nil: a missing metadata client
	// costs one feature, not the process.
	Meta metadata.Interface
}

// client-go defaults to QPS 5 / Burst 10, which is sized for a one-shot CLI. As a
// server that polls continuously — and, since the status handler runs its reads
// concurrently, in bursts — MatrixCtrl hit that client-side limiter constantly:
// requests were fast until the burst was spent and then settled at a steady ~1.1 s
// of pure queueing, with the cluster itself idle.
//
// These values are still modest for a single admin server against one cluster.
const (
	k8sQPS   = 50
	k8sBurst = 100
)

func New() (*Client, error) {
	cfg, err := config()
	if err != nil {
		return nil, fmt.Errorf("k8s config: %w", err)
	}
	cfg.QPS = k8sQPS
	cfg.Burst = k8sBurst

	static, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("static client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}

	c := &Client{Static: static, Dynamic: dyn}

	// Nil-tolerant on purpose, mirroring internal/helm: the ownership report is
	// worth having and not worth refusing to start over.
	if meta, err := metadata.NewForConfig(cfg); err != nil {
		log.Printf("k8s: metadata client unavailable (%v) — the manual-edit report will be empty", err)
	} else {
		c.Meta = meta
	}

	return c, nil
}

func config() (*rest.Config, error) {
	// In-cluster when KUBERNETES_SERVICE_HOST is set
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return rest.InClusterConfig()
	}
	// Fall back to kubeconfig for local dev
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// serviceAccountNamespaceFile is where the kubelet projects the pod's own
// namespace. Every pod with a ServiceAccount has it, and this process already
// depends on that mount for its in-cluster credentials — so reading the namespace
// from it adds no new requirement.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// CurrentNamespace returns the namespace this process runs in, or "" when that
// cannot be determined.
//
// Empty is a supported answer, not a failure: outside the cluster there is no
// "own namespace" to speak of, and the one caller (the diagnostics page, etappe
// 40) simply reports one namespace instead of two. An env var overrides it so a
// developer can point a local run at something specific.
func CurrentNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("MATRIXCTRL_NAMESPACE")); ns != "" {
		return ns
	}
	b, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
