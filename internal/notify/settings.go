package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	mc "github.com/bxnnyg/matrixctrl/internal/mail"
)

// Reminder values.
const (
	ReminderOff = "off"
	Reminder6h  = "6h"
	Reminder24h = "24h"
)

// SMTP sources.
const (
	SMTPMas = "mas"
	SMTPOwn = "own"
)

// Own is an SMTP account of its own. The password is in the Secret, not here.
type Own struct {
	From       string `json:"from"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`
	Username   string `json:"username"`
}

// Settings is the saved configuration (instance_settings row "notifications"). Secrets —
// the webhook URL and the own SMTP password — are not part of it.
type Settings struct {
	WebhookEnabled bool            `json:"webhook_enabled"`
	EmailEnabled   bool            `json:"email_enabled"`
	Recipients     []string        `json:"recipients"`
	SMTPSource     string          `json:"smtp_source"`
	Own            Own             `json:"own"`
	Reminder       string          `json:"reminder"`
	Triggers       map[string]bool `json:"triggers"`
}

// Default is what an installation without saved settings has: everything off.
func Default() Settings {
	return Settings{
		Recipients: []string{}, SMTPSource: SMTPMas, Reminder: ReminderOff,
		Own:      Own{Port: 587, Encryption: "starttls"},
		Triggers: map[string]bool{},
	}
}

// Normalize fills gaps a saved row from an older version may have.
func (s Settings) Normalize() Settings {
	if s.SMTPSource == "" {
		s.SMTPSource = SMTPMas
	}
	if s.Reminder == "" {
		s.Reminder = ReminderOff
	}
	if s.Recipients == nil {
		s.Recipients = []string{}
	}
	t := map[string]bool{}
	for _, k := range Triggers {
		t[k] = s.Triggers[k]
	}
	s.Triggers = t
	return s
}

// Interval is the reminder distance; zero means no reminders.
func (s Settings) Interval() time.Duration {
	switch s.Reminder {
	case Reminder6h:
		return 6 * time.Hour
	case Reminder24h:
		return 24 * time.Hour
	}
	return 0
}

// Validate says what is wrong with settings about to be saved. hasURL and hasPassword
// tell whether the secrets exist (already stored, or arriving with this request).
func (s Settings) Validate(hasURL, hasPassword bool) error {
	switch s.Reminder {
	case ReminderOff, Reminder6h, Reminder24h:
	default:
		return fmt.Errorf("Erinnerung: %q ist nicht vorgesehen (aus, 6h oder 24h)", s.Reminder)
	}
	switch s.SMTPSource {
	case SMTPMas, SMTPOwn:
	default:
		return fmt.Errorf("SMTP-Quelle: %q ist nicht vorgesehen (mas oder own)", s.SMTPSource)
	}
	if len(s.Recipients) > 20 {
		return errors.New("Höchstens 20 Empfänger")
	}
	for _, r := range s.Recipients {
		if _, err := mail.ParseAddress(r); err != nil {
			return fmt.Errorf("Empfänger: %q ist keine E-Mail-Adresse", r)
		}
	}
	if s.WebhookEnabled && !hasURL {
		return errors.New("Webhook: ohne URL kann nichts gesendet werden")
	}
	if s.EmailEnabled {
		if len(s.Recipients) == 0 {
			return errors.New("E-Mail: ohne Empfänger kann nichts gesendet werden")
		}
		if s.SMTPSource == SMTPOwn {
			if err := s.OwnMail("x").Validate(); err != nil {
				return err
			}
			if s.Own.Username != "" && !hasPassword {
				return errors.New("Zu einem Benutzernamen gehört ein Passwort")
			}
		}
	}
	return nil
}

// OwnMail is the own SMTP account as the mail package takes it.
func (s Settings) OwnMail(password string) mc.Settings {
	enc := mc.Encryption(s.Own.Encryption)
	if enc == "" {
		enc = mc.DefaultEncryption(s.Own.Port)
	}
	return mc.Settings{Enabled: true, From: s.Own.From, Host: s.Own.Host, Port: s.Own.Port,
		Encryption: enc, Username: s.Own.Username, Password: password}
}

// ValidateWebhookURL accepts an http(s) URL with a host.
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("Webhook-URL: eine vollständige http(s)-Adresse erwartet")
	}
	return nil
}

// SettingsStore keeps Settings.
type SettingsStore interface {
	Load(ctx context.Context) (Settings, error)
	Save(ctx context.Context, s Settings) error
}

// Secrets are the values that never leave the cluster Secret.
type Secrets struct {
	WebhookURL   string
	SMTPPassword string
}

// SecretStore keeps Secrets (a Kubernetes Secret in production).
type SecretStore interface {
	Get(ctx context.Context) (Secrets, error)
	Put(ctx context.Context, s Secrets) error
}
