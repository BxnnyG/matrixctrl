package k8s

import (
	"context"
	"fmt"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Who this process is talking to the API server as.
//
// Etappe 102 verified the media export "live" and the feature was still dead in
// production: the verification ran from a shell with a cluster-admin kubeconfig,
// while the application runs as a service account that was not allowed to exec into
// a pod at all. The measurement was true and answered a question nobody had asked.
//
// A permission check is only worth its green tick if it is asked about the identity
// that will actually make the call, so that identity has to be knowable (§4.104).
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	rev, err := c.Static.AuthenticationV1().SelfSubjectReviews().
		Create(ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("self subject review: %w", err)
	}
	return rev.Status.UserInfo.Username, nil
}

// ServiceAccountUser is the identity the deployed process runs as — the one every
// permission question in this package is actually about.
func ServiceAccountUser(namespace string) string {
	return "system:serviceaccount:" + namespace + ":matrixctrl"
}

// As returns a second client that asks the API server questions *as* another user.
//
// This exists for the permission checks and nothing else. Run inside the cluster
// they already speak as the service account; run from a maintainer's shell they
// speak as a cluster admin, and every answer is then about the wrong subject.
// Impersonating closes that gap instead of leaving the check to be believed.
//
// It needs the `impersonate` verb, which MatrixCtrl's own role deliberately does not
// have (it is in ForbiddenAlways) — so this works from an admin's kubeconfig and not
// from the pod, which is exactly the right way round: in the pod there is nothing to
// impersonate.
func (c *Client) As(user string) (*Client, error) {
	if c.rest == nil {
		return nil, fmt.Errorf("no rest config")
	}
	cfg := *c.rest
	cfg.Impersonate = c.rest.Impersonate
	cfg.Impersonate.UserName = user

	static, err := kubernetes.NewForConfig(&cfg)
	if err != nil {
		return nil, fmt.Errorf("impersonating client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(&cfg)
	if err != nil {
		return nil, fmt.Errorf("impersonating dynamic client: %w", err)
	}
	return &Client{Static: static, Dynamic: dyn, rest: &cfg}, nil
}
