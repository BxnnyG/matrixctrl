package notify

import (
	"context"
	"errors"
	"sync"
	"time"
)

type memStore struct {
	mu    sync.Mutex
	rows  map[string]Row
	chans map[string]ChannelStatus
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]Row{}, chans: map[string]ChannelStatus{}}
}

func (m *memStore) Rows(context.Context) ([]Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Row
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}
func (m *memStore) SaveRow(_ context.Context, r Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.Key] = r
	return nil
}
func (m *memStore) DeleteRow(_ context.Context, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, k)
	return nil
}
func (m *memStore) Channels(context.Context) ([]ChannelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ChannelStatus
	for _, c := range m.chans {
		out = append(out, c)
	}
	return out, nil
}
func (m *memStore) SaveChannel(_ context.Context, c ChannelStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chans[c.Channel] = c
	return nil
}

type memSettings struct{ s Settings }

func (m *memSettings) Load(context.Context) (Settings, error) { return m.s.Normalize(), nil }
func (m *memSettings) Save(_ context.Context, s Settings) error {
	m.s = s
	return nil
}

// recorder is a Sender that remembers messages and can be made to fail.
type recorder struct {
	mu   sync.Mutex
	got  []Message
	fail error
}

func (r *recorder) Send(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, m)
	return nil
}
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// fakeChecker answers with whatever the test sets.
type fakeChecker struct {
	trig   string
	alerts []Alert
	err    error
}

func (f *fakeChecker) Trigger() string                        { return f.trig }
func (f *fakeChecker) Check(context.Context) ([]Alert, error) { return f.alerts, f.err }

var errUnknown = errors.New("timeout")

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newClock() *clock { return &clock{t: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)} }

// rig is an engine with one fake checker and one recording webhook.
type rig struct {
	e     *Engine
	store *memStore
	set   *memSettings
	hook  *recorder
	ck    *fakeChecker
	clk   *clock
}

func newRig(trigger string) *rig {
	r := &rig{store: newMemStore(), hook: &recorder{}, clk: newClock(), ck: &fakeChecker{trig: trigger}}
	r.set = &memSettings{s: Settings{WebhookEnabled: true, Reminder: ReminderOff, Triggers: map[string]bool{trigger: true}}}
	r.e = r.engine()
	return r
}

// engine builds a fresh engine over the same store — what a restart does.
func (r *rig) engine() *Engine {
	return New(Deps{Settings: r.set, Store: r.store, Checkers: []Checker{r.ck}, Webhook: r.hook,
		Instance: func(context.Context) string { return "test.example" }, Now: r.clk.now})
}

func alert(key, trig string) Alert {
	return Alert{Key: key, Trigger: trig, Title: "T " + key, Detail: "D"}
}
