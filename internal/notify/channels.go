package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	mc "github.com/bxnnyg/matrixctrl/internal/mail"
)

// WebhookTimeout bounds one delivery.
const WebhookTimeout = 10 * time.Second

// WebhookSender posts {"text","html"} to the URL kept in the Secret. The URL is a
// credential (anyone holding it can post into the room): it is never put in an error.
type WebhookSender struct {
	Secrets SecretStore
	Client  *http.Client // nil: a default with WebhookTimeout
}

func (w *WebhookSender) Send(ctx context.Context, m Message) error {
	sec, err := w.Secrets.Get(ctx)
	if err != nil {
		return fmt.Errorf("Webhook-URL nicht lesbar: %w", err)
	}
	if strings.TrimSpace(sec.WebhookURL) == "" {
		return errors.New("Keine Webhook-URL hinterlegt")
	}
	body, _ := json.Marshal(map[string]string{"text": m.Text, "html": m.HTML})
	ctx, cancel := context.WithTimeout(ctx, WebhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sec.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("Die gespeicherte Webhook-URL ist ungültig")
	}
	req.Header.Set("Content-Type", "application/json")
	c := w.Client
	if c == nil {
		c = &http.Client{Timeout: WebhookTimeout}
	}
	resp, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // url.Error would repeat the URL
		}
		return fmt.Errorf("Webhook nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Webhook antwortete mit HTTP %d", resp.StatusCode)
	}
	return nil
}

// EmailSender mails the recipient list over the MAS account or an own one.
type EmailSender struct {
	Settings SettingsStore
	Secrets  SecretStore
	// MAS is the account MAS sends with; nil when there is no cluster.
	MAS func(ctx context.Context) (mc.Settings, error)
	// Send is mail.SendMessage; replaceable in tests.
	SendMail func(ctx context.Context, s mc.Settings, to, subject, body string) error
}

// MASAvailable reports whether the MAS account can be used.
func MASAvailable(ctx context.Context, mas func(context.Context) (mc.Settings, error)) bool {
	if mas == nil {
		return false
	}
	s, err := mas(ctx)
	return err == nil && s.Enabled && s.Validate() == nil
}

func (e *EmailSender) smtp(ctx context.Context, set Settings) (mc.Settings, error) {
	if set.SMTPSource == SMTPOwn {
		sec, err := e.Secrets.Get(ctx)
		if err != nil {
			return mc.Settings{}, fmt.Errorf("SMTP-Passwort nicht lesbar: %w", err)
		}
		s := set.OwnMail(sec.SMTPPassword)
		return s, s.Validate()
	}
	if e.MAS == nil {
		return mc.Settings{}, errors.New("Ohne Cluster-Zugriff ist der MAS-SMTP-Zugang nicht lesbar")
	}
	s, err := e.MAS(ctx)
	if err != nil {
		return mc.Settings{}, fmt.Errorf("MAS-SMTP-Zugang nicht lesbar: %w", err)
	}
	if !s.Enabled {
		return mc.Settings{}, errors.New("Der MAS-SMTP-Zugang ist nicht eingerichtet (Seite E-Mail) — eigenen Zugang eintragen oder dort einrichten")
	}
	return s, s.Validate()
}

// deliver tries every recipient and returns one error per failure.
func (e *EmailSender) deliver(ctx context.Context, m Message) (int, []error) {
	set, err := e.Settings.Load(ctx)
	if err != nil {
		return 0, []error{err}
	}
	set = set.Normalize()
	if len(set.Recipients) == 0 {
		return 0, []error{errors.New("Keine Empfänger eingetragen")}
	}
	smtp, err := e.smtp(ctx, set)
	if err != nil {
		return 0, []error{err}
	}
	send := e.SendMail
	if send == nil {
		send = mc.SendMessage
	}
	ok := 0
	var errs []error
	for _, to := range set.Recipients {
		if err := send(ctx, smtp, to, m.Subject, m.Text); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", to, err))
		} else {
			ok++
		}
	}
	return ok, errs
}

// Send succeeds when at least one recipient was reached; a bad address must not make
// the others get the same mail every minute.
func (e *EmailSender) Send(ctx context.Context, m Message) error {
	ok, errs := e.deliver(ctx, m)
	if ok > 0 {
		return nil
	}
	return errors.Join(errs...)
}

// SendStrict is for the test button: every failure is reported.
func (e *EmailSender) SendStrict(ctx context.Context, m Message) error {
	_, errs := e.deliver(ctx, m)
	return errors.Join(errs...)
}
