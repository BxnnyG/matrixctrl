// Package notify makes the panel speak up (etappe 123): a problem is reported once when
// it appears, optionally again at an interval while it lasts, and once more when it is
// gone. Checkers say what is wrong *now*; the engine compares that with what it has
// already told the operator (notify_state) and sends the difference.
//
// Two rules carry the design:
//   - a checker that cannot read its source answers "unknown" (an error): neither an
//     alert nor a resolution — a slow cluster must not page anyone, and must not call
//     a real problem solved;
//   - a delivery that failed is not recorded as sent, so the next run tries again.
package notify

import (
	"context"
	"time"
)

// Trigger names a kind of problem the operator can switch on or off.
const (
	TriggerDNS    = "dns"
	TriggerCert   = "cert"
	TriggerUpdate = "update"
	TriggerPod    = "pod"
	TriggerBackup = "backup"
)

// Triggers lists every trigger in display order.
var Triggers = []string{TriggerDNS, TriggerCert, TriggerUpdate, TriggerPod, TriggerBackup}

// Channel names.
const (
	ChannelWebhook = "webhook"
	ChannelEmail   = "email"
)

// Alert is one problem that exists right now.
type Alert struct {
	// Key identifies the problem across runs ("dns", "pod:ess-postgres").
	Key     string
	Trigger string
	Title   string
	Detail  string
	// Once marks a notice rather than a fault (a new version): no reminder, and no
	// "resolved" message when it goes away.
	Once bool
}

// Checker reports the alerts that exist now for one trigger. An error means unknown.
type Checker interface {
	Trigger() string
	Check(ctx context.Context) ([]Alert, error)
}

// Row is what has been told about one alert.
type Row struct {
	Key     string
	Trigger string
	Title   string
	Detail  string
	Once    bool
	Since   time.Time
	// Sent is, per channel, when the problem (or its latest reminder) was delivered
	// there; a channel without an entry has not received it yet.
	Sent map[string]time.Time
	// ResolvedSent is, per channel, when the "resolved" message arrived.
	ResolvedSent map[string]time.Time
	// Resolved is set when the alert is gone but the "resolved" message is still owed.
	Resolved *time.Time
}

// LastSent is the latest delivery over any channel, nil when none.
func (r Row) LastSent() *time.Time {
	var out *time.Time
	for _, t := range r.Sent {
		t := t
		if out == nil || t.After(*out) {
			out = &t
		}
	}
	return out
}

// ChannelStatus is what the page shows per channel.
type ChannelStatus struct {
	Channel     string
	LastSentAt  *time.Time
	LastError   string
	LastErrorAt *time.Time
}

// Store persists state (Postgres in production).
type Store interface {
	Rows(ctx context.Context) ([]Row, error)
	SaveRow(ctx context.Context, r Row) error
	DeleteRow(ctx context.Context, key string) error
	Channels(ctx context.Context) ([]ChannelStatus, error)
	SaveChannel(ctx context.Context, c ChannelStatus) error
}

// Message is what a channel delivers.
type Message struct {
	Subject string
	Text    string
	HTML    string
}

// Sender delivers a message over one channel.
type Sender interface {
	Send(ctx context.Context, m Message) error
}
