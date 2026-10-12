// Package bridges reads appservice registrations — the files through which Synapse
// knows a bridge — and says whether the bridge behind one answers (etappe 119).
//
// A registration carries two tokens: the one the bridge uses to act as its users, and
// the one Synapse uses to reach the bridge. They never leave this package. Everything
// else in the file is what an operator needs to recognise which bridge it is.
package bridges

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"gopkg.in/yaml.v3"
)

// Registration is the readable part of an appservice registration.
type Registration struct {
	ID              string   `json:"id"`
	URL             string   `json:"url,omitempty"`
	SenderLocalpart string   `json:"sender_localpart"`
	UserNamespaces  []string `json:"user_namespaces,omitempty"`
	AliasNamespaces []string `json:"alias_namespaces,omitempty"`
	Protocols       []string `json:"protocols,omitempty"`
	RateLimited     *bool    `json:"rate_limited,omitempty"`
}

type namespace struct {
	Regex     string `yaml:"regex"`
	Exclusive bool   `yaml:"exclusive"`
}

type wire struct {
	ID              string  `yaml:"id"`
	URL             *string `yaml:"url"`
	ASToken         string  `yaml:"as_token"`
	HSToken         string  `yaml:"hs_token"`
	SenderLocalpart string  `yaml:"sender_localpart"`
	RateLimited     *bool   `yaml:"rate_limited"`
	Protocols       []string
	Namespaces      struct {
		Users   []namespace `yaml:"users"`
		Aliases []namespace `yaml:"aliases"`
		Rooms   []namespace `yaml:"rooms"`
	} `yaml:"namespaces"`
}

// Parse reads a registration and checks what Synapse itself requires of one. The tokens
// are checked for presence and then dropped.
func Parse(raw []byte) (Registration, error) {
	var w wire
	if err := yaml.Unmarshal(raw, &w); err != nil {
		return Registration{}, fmt.Errorf("keine gültige Registrierung (YAML): %w", err)
	}
	var missing []string
	if w.ID == "" {
		missing = append(missing, "id")
	}
	if w.ASToken == "" {
		missing = append(missing, "as_token")
	}
	if w.HSToken == "" {
		missing = append(missing, "hs_token")
	}
	if w.SenderLocalpart == "" {
		missing = append(missing, "sender_localpart")
	}
	if len(missing) > 0 {
		return Registration{}, fmt.Errorf("es fehlt: %v", missing)
	}
	r := Registration{ID: w.ID, SenderLocalpart: w.SenderLocalpart, RateLimited: w.RateLimited, Protocols: w.Protocols}
	if w.URL != nil {
		r.URL = *w.URL
	}
	for _, n := range w.Namespaces.Users {
		r.UserNamespaces = append(r.UserNamespaces, n.Regex)
	}
	for _, n := range w.Namespaces.Aliases {
		r.AliasNamespaces = append(r.AliasNamespaces, n.Regex)
	}
	return r, nil
}

// Reach says whether something accepts connections at a registration's URL — from this
// pod, which is inside the cluster like Synapse. "" means it does.
//
// Only a connection, no request: what a bridge answers to an unauthenticated GET is its
// own business, and the question here is the one that breaks bridges in practice — the
// address Synapse was given does not lead anywhere (a Service renamed, a namespace moved).
func Reach(ctx context.Context, rawURL string, dial func(ctx context.Context, network, addr string) (net.Conn, error)) string {
	if rawURL == "" {
		return "keine Adresse — Synapse schickt dieser Bridge nichts"
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "keine gültige Adresse"
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 3 * time.Second}
		dial = d.DialContext
	}
	c, err := dial(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return "der Name " + u.Hostname() + " ist im Cluster unbekannt"
		}
		return "keine Verbindung: " + err.Error()
	}
	_ = c.Close()
	return ""
}
