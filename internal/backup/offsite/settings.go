package offsite

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Settings is everything about the target and the schedule except the secrets: the S3
// access keys live in a Kubernetes Secret, and the private age key is never stored at all.
type Settings struct {
	Target
	// PublicKey is the age recipient (`age1…`). Empty means no key, which means no backup:
	// an unencrypted archive in a bucket is the one outcome this feature must not produce.
	PublicKey string `json:"public_key"`
	// Schedule turns the daily run on; ScheduleTime is HH:MM in UTC.
	Schedule     bool   `json:"schedule"`
	ScheduleTime string `json:"schedule_time"`
	// Media says whether scheduled and manual backups include the uploaded files. The
	// backup taken before an upgrade ignores it: an upgrade does not touch them.
	Media bool `json:"media"`
}

// DefaultSettings is what a fresh installation shows.
func DefaultSettings() Settings {
	return Settings{
		Target:       Target{Prefix: "matrixctrl"},
		Schedule:     true,
		ScheduleTime: "03:30",
		Media:        true,
	}
}

// ParseScheduleTime reads HH:MM.
func ParseScheduleTime(s string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, errors.New("Uhrzeit: bitte als HH:MM angeben, z. B. 03:30 (UTC)")
	}
	return t.Hour(), t.Minute(), nil
}

// Validate checks what can be checked without the network.
func (s Settings) Validate() error {
	if _, _, err := ParseEndpoint(s.Endpoint); err != nil {
		return err
	}
	if s.Bucket == "" {
		return errors.New("Bucket: bitte den Namen des Buckets angeben")
	}
	if _, err := NormalizePrefix(s.Prefix); err != nil {
		return err
	}
	if _, _, err := ParseScheduleTime(s.ScheduleTime); err != nil {
		return err
	}
	if s.PublicKey != "" {
		if _, err := ParsePublicKey(s.PublicKey); err != nil {
			return err
		}
	}
	return nil
}

// Missing lists, in words, what stops a backup from being possible. Empty means it is
// set up.
func (s Settings) Missing(c Credentials) []string {
	var m []string
	if s.Endpoint == "" {
		m = append(m, "Endpunkt")
	}
	if s.Bucket == "" {
		m = append(m, "Bucket")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		m = append(m, "Zugangsschlüssel")
	}
	if s.PublicKey == "" {
		m = append(m, "Verschlüsselungsschlüssel")
	}
	return m
}

// SettingsStore persists Settings (instance_settings in production).
type SettingsStore interface {
	Load(ctx context.Context) (Settings, error)
	Save(ctx context.Context, s Settings) error
}

// CredStore persists the S3 access keys (a Kubernetes Secret in production).
type CredStore interface {
	Get(ctx context.Context) (Credentials, error)
	Put(ctx context.Context, c Credentials) error
}

// SecretAPI is the slice of the cluster client the credential store needs.
type SecretAPI interface {
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
	PutSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
}

// SecretName is the Kubernetes Secret holding the access keys.
const SecretName = "matrixctrl-backup-s3"

type k8sCreds struct {
	api SecretAPI
	ns  string
}

// NewK8sCreds keeps the access keys in the Secret SecretName in namespace ns, like the
// SMTP password (internal/mail) and the login providers' client secrets.
func NewK8sCreds(api SecretAPI, ns string) CredStore { return &k8sCreds{api: api, ns: ns} }

func (k *k8sCreds) Get(ctx context.Context) (Credentials, error) {
	d, err := k.api.GetSecret(ctx, k.ns, SecretName)
	if err != nil {
		return Credentials{}, fmt.Errorf("Zugangsschlüssel nicht lesbar: %w", err)
	}
	return Credentials{AccessKey: string(d["access_key"]), SecretKey: string(d["secret_key"])}, nil
}

func (k *k8sCreds) Put(ctx context.Context, c Credentials) error {
	d, err := k.api.GetSecret(ctx, k.ns, SecretName)
	if err != nil {
		return fmt.Errorf("Zugangsschlüssel nicht lesbar: %w", err)
	}
	if d == nil {
		d = map[string][]byte{}
	}
	d["access_key"] = []byte(c.AccessKey)
	d["secret_key"] = []byte(c.SecretKey)
	if err := k.api.PutSecret(ctx, k.ns, SecretName, d); err != nil {
		return fmt.Errorf("Zugangsschlüssel nicht speicherbar: %w", err)
	}
	return nil
}
