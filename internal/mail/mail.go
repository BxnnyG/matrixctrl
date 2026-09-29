// Package mail configures the e-mail MAS sends — registration confirmations and
// forgotten passwords (etappe 114b).
//
// Two settings of the task layer are inert without it: "require an e-mail address at
// registration" and password recovery. MAS takes SMTP credentials in its own
// configuration, password in clear text, so the whole `email:` block lives in a
// Kubernetes Secret like the login providers' client secrets (§4.112) and never in the
// settings repository.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Encryption is how the connection to the mail server is protected.
type Encryption string

const (
	// TLS wraps the whole connection (port 465).
	TLS Encryption = "tls"
	// StartTLS upgrades a plain connection (port 587), and fails if the server does
	// not offer it — an upgrade that silently does not happen is worse than no upgrade.
	StartTLS Encryption = "starttls"
	// None is plain SMTP (port 25): for a mail server inside the cluster.
	None Encryption = "plain"
)

// Settings is the mail configuration as the assistant keeps it.
type Settings struct {
	Enabled    bool       `json:"enabled"`
	From       string     `json:"from"`
	ReplyTo    string     `json:"reply_to,omitempty"`
	Host       string     `json:"host"`
	Port       int        `json:"port"`
	Encryption Encryption `json:"encryption"`
	Username   string     `json:"username,omitempty"`
	Password   string     `json:"password,omitempty"`
}

// DefaultEncryption is what a port usually means. Only a suggestion: the operator can
// change it, and a server on a non-standard port is not wrong.
func DefaultEncryption(port int) Encryption {
	switch port {
	case 465:
		return TLS
	case 25:
		return None
	default:
		return StartTLS
	}
}

// Validate reports what would stop MAS from starting, or the operator from receiving
// anything.
func (s Settings) Validate() error {
	if !s.Enabled {
		return nil
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("Absender: %q ist keine E-Mail-Adresse (z. B. noreply@example.org oder \"Mein Server\" <noreply@example.org>)", s.From)
	}
	if s.ReplyTo != "" {
		if _, err := mail.ParseAddress(s.ReplyTo); err != nil {
			return fmt.Errorf("Antwort-an: %q ist keine E-Mail-Adresse", s.ReplyTo)
		}
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("Ohne Mailserver kann nichts versendet werden")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("Port: eine Zahl zwischen 1 und 65535 erwartet")
	}
	switch s.Encryption {
	case TLS, StartTLS, None:
	default:
		return fmt.Errorf("Unbekannte Verschlüsselung %q", s.Encryption)
	}
	if s.Username != "" && s.Password == "" {
		return fmt.Errorf("Zu einem Benutzernamen gehört ein Passwort")
	}
	return nil
}

// Render is the `email:` block MAS reads.
//
// Disabled is `transport: blackhole` — MAS's own word for "send nothing" — with the
// other values kept, so switching back on does not mean typing everything again.
func Render(s Settings) (string, error) {
	email := map[string]interface{}{"transport": "blackhole"}
	if s.From != "" {
		email["from"] = s.From
	}
	if s.ReplyTo != "" {
		email["reply_to"] = s.ReplyTo
	}
	if s.Enabled {
		email["transport"] = "smtp"
		email["mode"] = string(s.Encryption)
		email["hostname"] = s.Host
		email["port"] = s.Port
		if s.Username != "" {
			email["username"] = s.Username
			email["password"] = s.Password
		}
	}
	out, err := yaml.Marshal(map[string]interface{}{"email": email})
	return string(out), err
}

// Parse reads back what Render wrote.
func Parse(doc string) (Settings, error) {
	var v struct {
		Email struct {
			From      string `yaml:"from"`
			ReplyTo   string `yaml:"reply_to"`
			Transport string `yaml:"transport"`
			Mode      string `yaml:"mode"`
			Hostname  string `yaml:"hostname"`
			Port      int    `yaml:"port"`
			Username  string `yaml:"username"`
			Password  string `yaml:"password"`
		} `yaml:"email"`
	}
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		return Settings{}, err
	}
	e := v.Email
	s := Settings{
		Enabled: e.Transport == "smtp", From: e.From, ReplyTo: e.ReplyTo,
		Host: e.Hostname, Port: e.Port, Encryption: Encryption(e.Mode),
		Username: e.Username, Password: e.Password,
	}
	if s.Encryption == "" {
		s.Encryption = DefaultEncryption(s.Port)
	}
	if s.Port == 0 {
		s.Port = 587
	}
	return s, nil
}

const probeTimeout = 10 * time.Second

// Probe opens the connection MAS would open — greeting, encryption, log in — and hangs
// up without sending anything. Answers "will this work?" before the first user depends
// on it.
func Probe(ctx context.Context, s Settings) error {
	c, err := connect(ctx, s)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Quit()
}

// Send delivers one real message: the only proof that arrives in an inbox.
func Send(ctx context.Context, s Settings, to string) error {
	addr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("%q ist keine E-Mail-Adresse", to)
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("Absender: %w", err)
	}
	c, err := connect(ctx, s)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("Der Mailserver hat den Absender %s abgelehnt: %w", from.Address, err)
	}
	if err := c.Rcpt(addr.Address); err != nil {
		return fmt.Errorf("Der Mailserver hat den Empfänger %s abgelehnt: %w", addr.Address, err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Testnachricht von MatrixCtrl\r\n"+
		"Date: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"+
		"Diese Nachricht bestätigt, dass dein Matrix-Server E-Mails versenden kann.\r\n"+
		"Sie wurde in den Einstellungen unter „E-Mail\" ausgelöst.\r\n",
		s.From, addr.Address, time.Now().Format(time.RFC1123Z))
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("Der Mailserver hat die Nachricht abgelehnt: %w", err)
	}
	return c.Quit()
}

// isLocal mirrors what net/smtp accepts as "the password does not leave this machine".
func isLocal(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasSuffix(host, ".svc.cluster.local") || strings.HasSuffix(host, ".local")
}

func connect(ctx context.Context, s Settings) (*smtp.Client, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	d := &net.Dialer{Timeout: probeTimeout}
	var conn net.Conn
	var err error
	if s.Encryption == TLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: s.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("Keine Verbindung zu %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(probeTimeout))

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s antwortet nicht wie ein Mailserver: %w", addr, err)
	}
	if s.Encryption == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			c.Close()
			return nil, fmt.Errorf("%s bietet kein STARTTLS an — ohne Verschlüsselung würde das Passwort im Klartext übertragen. Port 465 (TLS) versuchen, oder die Verschlüsselung bewusst auf „keine\" stellen", addr)
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			c.Close()
			return nil, fmt.Errorf("Verschlüsselung mit %s fehlgeschlagen: %w", s.Host, err)
		}
	}
	if s.Username != "" {
		// Go refuses PLAIN over an unencrypted connection to anything but localhost,
		// and it is right to: the password would cross the network in clear. Said here
		// in words, because "unencrypted connection" as a bare error reads like a bug.
		if s.Encryption == None && !isLocal(s.Host) {
			c.Close()
			return nil, fmt.Errorf("Ohne Verschlüsselung wird das Passwort im Klartext übertragen — deshalb ist die Anmeldung nur bei einem Mailserver im selben Cluster erlaubt. Für %s bitte STARTTLS (Port 587) oder TLS (Port 465) wählen", s.Host)
		}
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			c.Close()
			return nil, fmt.Errorf("Anmeldung als %s abgelehnt: %w", s.Username, err)
		}
	}
	return c, nil
}
