package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// GetSecret returns every entry of a Secret, or an empty map if there is none.
//
// Absent is not an error here: restoring the homeserver's keys onto a cluster that has
// not generated any yet is a normal migration, and the caller merges against what it
// gets.
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	s, err := c.Static.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Data, nil
}

// PutSecret writes a Secret's entries, creating it if it does not exist.
//
// Update rather than replace: the object carries labels and annotations that belong to
// the Helm release, and a restore that drops them makes the next `helm upgrade` refuse
// to adopt an object it thinks it does not own (§4.89).
func (c *Client) PutSecret(ctx context.Context, namespace, name string, data map[string][]byte) error {
	secrets := c.Static.CoreV1().Secrets(namespace)

	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Data:       data,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Data = data
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// SecretVolumes lists the Secrets a Deployment's pods mount, directly or projected
// (etappe 110: is MAS reading the login-provider Secret yet?).
func (c *Client) SecretVolumes(ctx context.Context, namespace, deployment string) ([]string, error) {
	d, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.Secret != nil {
			out = append(out, v.Secret.SecretName)
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.Secret != nil {
					out = append(out, src.Secret.Name)
				}
			}
		}
	}
	return out, nil
}
