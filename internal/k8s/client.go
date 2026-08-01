package k8s

import (
	"fmt"
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Static  *kubernetes.Clientset
	Dynamic dynamic.Interface
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

	return &Client{Static: static, Dynamic: dyn}, nil
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
