package notify

import (
	"context"
	"fmt"
)

// SecretName is the Kubernetes Secret holding the webhook URL and the own SMTP password.
const SecretName = "matrixctrl-notify"

// SecretAPI is the slice of the cluster client the secret store needs.
type SecretAPI interface {
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
	PutSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
}

type k8sSecrets struct {
	api SecretAPI
	ns  string
}

// NewK8sSecrets keeps Secrets in the Secret SecretName of namespace ns.
func NewK8sSecrets(api SecretAPI, ns string) SecretStore { return &k8sSecrets{api: api, ns: ns} }

func (k *k8sSecrets) Get(ctx context.Context) (Secrets, error) {
	d, err := k.api.GetSecret(ctx, k.ns, SecretName)
	if err != nil {
		return Secrets{}, fmt.Errorf("Secret %s nicht lesbar: %w", SecretName, err)
	}
	return Secrets{WebhookURL: string(d["webhook_url"]), SMTPPassword: string(d["smtp_password"])}, nil
}

func (k *k8sSecrets) Put(ctx context.Context, s Secrets) error {
	d := map[string][]byte{}
	if s.WebhookURL != "" {
		d["webhook_url"] = []byte(s.WebhookURL)
	}
	if s.SMTPPassword != "" {
		d["smtp_password"] = []byte(s.SMTPPassword)
	}
	return k.api.PutSecret(ctx, k.ns, SecretName, d)
}
