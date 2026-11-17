package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
	"sync"
	"time"
)

// Deps are the engine's collaborators; all are replaceable in tests.
type Deps struct {
	Settings SettingsStore
	Store    Store
	Checkers []Checker
	// Webhook and Email may be nil (no cluster: the secret cannot be kept).
	Webhook Sender
	Email   Sender
	// Instance is the name put in front of every message.
	Instance func(ctx context.Context) string
	Now      func() time.Time
}

// LastRun is what the page shows about the latest pass.
type LastRun struct {
	At time.Time `json:"at"`
	// Unknown lists triggers whose source could not be read in that pass.
	Unknown []string `json:"unknown"`
	Error   string   `json:"error,omitempty"`
}

// Engine runs the checkers and sends the difference.
type Engine struct {
	d       Deps
	mu      sync.Mutex // one pass at a time
	lastMu  sync.Mutex
	lastRun *LastRun
}

const (
	checkTimeout = 30 * time.Second
	sendTimeout  = 15 * time.Second
)

func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Instance == nil {
		d.Instance = func(context.Context) string { return "MatrixCtrl" }
	}
	return &Engine{d: d}
}

// Run passes once a minute until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	e.Once(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Once(ctx)
		}
	}
}

// LastRun is the latest pass, nil before the first.
func (e *Engine) LastRun() *LastRun {
	e.lastMu.Lock()
	defer e.lastMu.Unlock()
	if e.lastRun == nil {
		return nil
	}
	c := *e.lastRun
	return &c
}

func (e *Engine) senders(set Settings) map[string]Sender {
	out := map[string]Sender{}
	if set.WebhookEnabled && e.d.Webhook != nil {
		out[ChannelWebhook] = e.d.Webhook
	}
	if set.EmailEnabled && e.d.Email != nil {
		out[ChannelEmail] = e.d.Email
	}
	return out
}

// Once does one pass.
func (e *Engine) Once(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	run := LastRun{At: e.d.Now().UTC(), Unknown: []string{}}
	defer func() {
		e.lastMu.Lock()
		e.lastRun = &run
		e.lastMu.Unlock()
	}()

	set, err := e.d.Settings.Load(ctx)
	if err != nil {
		run.Error = "Einstellungen nicht lesbar: " + err.Error()
		return
	}
	set = set.Normalize()
	rows, err := e.d.Store.Rows(ctx)
	if err != nil {
		run.Error = "Zustand nicht lesbar: " + err.Error()
		return
	}
	byKey := map[string]Row{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	senders := e.senders(set)
	instance := e.d.Instance(ctx)
	now := e.d.Now()

	handled := map[string]bool{} // triggers whose rows were dealt with
	for _, ck := range e.d.Checkers {
		trig := ck.Trigger()
		handled[trig] = true
		if !set.Triggers[trig] {
			// Switched off: forget silently. Switching on again announces afresh.
			for _, r := range rows {
				if r.Trigger == trig {
					_ = e.d.Store.DeleteRow(ctx, r.Key)
				}
			}
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, checkTimeout)
		alerts, err := ck.Check(cctx)
		cancel()
		if err != nil {
			run.Unknown = append(run.Unknown, trig)
			continue
		}
		current := map[string]bool{}
		for _, a := range alerts {
			current[a.Key] = true
			e.alertSeen(ctx, a, byKey[a.Key], set, senders, instance, now)
		}
		for _, r := range rows {
			if r.Trigger == trig && !current[r.Key] {
				e.alertGone(ctx, r, senders, instance, now)
			}
		}
	}
	// Rows of a trigger no checker serves any more (e.g. update check disabled).
	for _, r := range rows {
		if !handled[r.Trigger] {
			_ = e.d.Store.DeleteRow(ctx, r.Key)
		}
	}
	sort.Strings(run.Unknown)
}

func (e *Engine) alertSeen(ctx context.Context, a Alert, row Row, set Settings, senders map[string]Sender, instance string, now time.Time) {
	exists := row.Key != ""
	if !exists {
		row = Row{Key: a.Key, Trigger: a.Trigger, Since: now}
	}
	changed := !exists || row.Title != a.Title || row.Detail != a.Detail || row.Once != a.Once || row.Resolved != nil
	row.Title, row.Detail, row.Once, row.Resolved = a.Title, a.Detail, a.Once, nil

	var kind string
	switch {
	case row.LastSent == nil:
		kind = "problem"
	case !a.Once && set.Interval() > 0 && now.Sub(*row.LastSent) >= set.Interval():
		kind = "reminder"
	}
	if kind != "" && e.deliver(ctx, senders, compose(kind, instance, row, now), now) {
		t := now
		row.LastSent = &t
		changed = true
	}
	if changed {
		_ = e.d.Store.SaveRow(ctx, row)
	}
}

func (e *Engine) alertGone(ctx context.Context, r Row, senders map[string]Sender, instance string, now time.Time) {
	// Never announced, or a notice rather than a fault: nothing to take back.
	if r.Once || r.LastSent == nil || len(senders) == 0 {
		_ = e.d.Store.DeleteRow(ctx, r.Key)
		return
	}
	if e.deliver(ctx, senders, compose("resolved", instance, r, now), now) {
		_ = e.d.Store.DeleteRow(ctx, r.Key)
		return
	}
	if r.Resolved == nil {
		t := now
		r.Resolved = &t
		_ = e.d.Store.SaveRow(ctx, r)
	}
}

// deliver sends over every enabled channel and records each outcome. It counts as
// delivered when at least one channel took it: with two channels, one dead webhook
// must not mean an e-mail every minute.
func (e *Engine) deliver(ctx context.Context, senders map[string]Sender, m Message, now time.Time) bool {
	ok := false
	for _, name := range []string{ChannelWebhook, ChannelEmail} {
		s := senders[name]
		if s == nil {
			continue
		}
		if err := e.sendOne(ctx, name, s, m, now); err == nil {
			ok = true
		}
	}
	return ok
}

func (e *Engine) sendOne(ctx context.Context, name string, s Sender, m Message, now time.Time) error {
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	err := s.Send(sctx, m)
	st := ChannelStatus{Channel: name}
	if cur, cerr := e.d.Store.Channels(ctx); cerr == nil {
		for _, c := range cur {
			if c.Channel == name {
				st = c
			}
		}
	}
	t := now
	if err != nil {
		st.LastError, st.LastErrorAt = err.Error(), &t
	} else {
		st.LastSentAt, st.LastError, st.LastErrorAt = &t, "", nil
	}
	_ = e.d.Store.SaveChannel(ctx, st)
	return err
}

// Test sends a test message over one channel and returns the error verbatim.
func (e *Engine) Test(ctx context.Context, channel string) error {
	var s Sender
	switch channel {
	case ChannelWebhook:
		s = e.d.Webhook
	case ChannelEmail:
		s = e.d.Email
	default:
		return fmt.Errorf("Unbekannter Kanal %q", channel)
	}
	if s == nil {
		return errors.New("Dieser Kanal ist ohne Cluster-Zugriff nicht nutzbar")
	}
	if ss, ok := s.(interface {
		SendStrict(ctx context.Context, m Message) error
	}); ok {
		s = strictSender{ss}
	}
	instance := e.d.Instance(ctx)
	text := fmt.Sprintf("[%s] Testnachricht: Benachrichtigungen von MatrixCtrl kommen hier an.", instance)
	return e.sendOne(ctx, channel, s, Message{
		Subject: fmt.Sprintf("[%s] Testnachricht", instance),
		Text:    text,
		HTML:    "<b>" + html.EscapeString(fmt.Sprintf("[%s]", instance)) + "</b> Testnachricht: Benachrichtigungen von MatrixCtrl kommen hier an.",
	}, e.d.Now())
}

// Snapshot is what the state route shows.
type Snapshot struct {
	Alerts   []Row
	Channels map[string]ChannelStatus
	LastRun  *LastRun
}

func (e *Engine) Snapshot(ctx context.Context) (Snapshot, error) {
	rows, err := e.d.Store.Rows(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	chans, err := e.d.Store.Channels(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{Channels: map[string]ChannelStatus{
		ChannelWebhook: {Channel: ChannelWebhook}, ChannelEmail: {Channel: ChannelEmail},
	}, LastRun: e.LastRun()}
	for _, r := range rows {
		if r.Resolved == nil {
			out.Alerts = append(out.Alerts, r)
		}
	}
	sort.Slice(out.Alerts, func(i, j int) bool { return out.Alerts[i].Since.Before(out.Alerts[j].Since) })
	for _, c := range chans {
		out.Channels[c.Channel] = c
	}
	return out, nil
}

// compose writes the message: German, short, with the instance name.
func compose(kind, instance string, r Row, now time.Time) Message {
	var head string
	switch kind {
	case "reminder":
		head = "Erinnerung"
	case "resolved":
		head = "Entwarnung"
	default:
		head = "Problem"
		if r.Once {
			head = "Hinweis"
		}
	}
	title := r.Title
	if kind == "resolved" {
		title += " (behoben)"
	}
	text := fmt.Sprintf("[%s] %s: %s", instance, head, title)
	if r.Detail != "" {
		text += "\n" + r.Detail
	}
	if kind == "reminder" {
		text += fmt.Sprintf("\nBesteht seit %s (%s).", r.Since.UTC().Format("02.01. 15:04 UTC"), since(now.Sub(r.Since)))
	}
	if kind == "resolved" {
		text += fmt.Sprintf("\nBestand seit %s.", r.Since.UTC().Format("02.01. 15:04 UTC"))
	}
	h := "<b>" + html.EscapeString(fmt.Sprintf("[%s] %s:", instance, head)) + "</b> " + html.EscapeString(title)
	if r.Detail != "" {
		h += "<br>" + strings.ReplaceAll(html.EscapeString(r.Detail), "\n", "<br>")
	}
	return Message{Subject: fmt.Sprintf("[%s] %s: %s", instance, head, title), Text: text, HTML: h}
}

func since(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d Tage", int(d.Hours()/24))
}

// strictSender makes the test button report every failed recipient, not only a total one.
type strictSender struct {
	s interface {
		SendStrict(ctx context.Context, m Message) error
	}
}

func (s strictSender) Send(ctx context.Context, m Message) error { return s.s.SendStrict(ctx, m) }
