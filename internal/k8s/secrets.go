package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretValue reads one key out of a Secret (etappe 70).
//
// Returned rather than cached: a credential held for the life of the process outlives
// the reason it was needed, and this one is read per export. Nothing here logs the
// value, and callers must not either — an error carrying a DSN carries a password.
func (c *Client) SecretValue(ctx context.Context, namespace, name, key string) (string, error) {
	if c == nil || c.Static == nil {
		return "", fmt.Errorf("no cluster access")
	}
	sec, err := c.Static.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("secret %s/%s: %w", namespace, name, err)
	}
	v, ok := sec.Data[key]
	if !ok {
		// Naming the key is safe; naming the value never is.
		return "", fmt.Errorf("secret %s/%s has no key %q", namespace, name, key)
	}
	return string(v), nil
}
