package offsite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

// RunRecord is one row of offsite_runs.
type RunRecord struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Occasion   string     `json:"occasion"`
	// Status is running | ok | failed | skipped.
	Status string `json:"status"`
	Object string `json:"object,omitempty"`
	Size   int64  `json:"size_bytes"`
	Media  bool   `json:"media"`
	Error  string `json:"error,omitempty"`
	Note   string `json:"note,omitempty"`
}

// DurationSeconds is how long the run took, 0 while it is running.
func (r RunRecord) DurationSeconds() float64 {
	if r.FinishedAt == nil {
		return 0
	}
	return r.FinishedAt.Sub(r.StartedAt).Seconds()
}

// RunStore persists the run history (offsite_runs in production).
type RunStore interface {
	Insert(ctx context.Context, r RunRecord) (int64, error)
	Update(ctx context.Context, r RunRecord) error
	Recent(ctx context.Context, n int) ([]RunRecord, error)
	LastOK(ctx context.Context) (*RunRecord, error)
	LastStart(ctx context.Context, occasion string) (time.Time, error)
	CloseInterrupted(ctx context.Context) (int64, error)
}

// ErrBusy: another backup is running. Two at once would read the same databases twice and
// could prune each other's fresh objects.
var ErrBusy = errors.New("Es läuft bereits ein Backup.")

// NotConfiguredError says what is missing.
type NotConfiguredError struct{ Missing []string }

func (e *NotConfiguredError) Error() string {
	return fmt.Sprintf("Das Backup-Ziel ist nicht fertig eingerichtet; es fehlt: %s.", joinWords(e.Missing))
}

func joinWords(w []string) string {
	out := ""
	for i, s := range w {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// Deps are the service's collaborators; all are replaceable in tests.
type Deps struct {
	Settings SettingsStore
	Creds    CredStore // may be nil without cluster access
	Runs     RunStore
	NewStore func(Target, Credentials) (Store, error)
	// Build writes the plain archive; media says whether to include the uploaded files.
	Build  func(ctx context.Context, media bool, w io.Writer) error
	Policy Policy
	Now    func() time.Time
}

// Service ties settings, keys, the archive build and the bucket together.
type Service struct {
	d    Deps
	mu   sync.Mutex
	busy bool
	// since is when the running backup started.
	since time.Time
}

func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewStore == nil {
		d.NewStore = NewS3Store
	}
	if d.Policy == (Policy{}) {
		d.Policy = DefaultPolicy
	}
	return &Service{d: d}
}

// BackupRequest is one backup.
type BackupRequest struct {
	Occasion string
	Media    bool
	// Progress receives human-readable lines (the upgrade stream shows them).
	Progress func(string)
}

type prepared struct {
	st       Store
	settings Settings
	req      BackupRequest
	runID    int64
	rec      RunRecord
}

func (s *Service) creds(ctx context.Context) (Credentials, error) {
	if s.d.Creds == nil {
		return Credentials{}, errors.New("ohne Cluster-Zugriff lassen sich Zugangsschlüssel nicht ablegen")
	}
	return s.d.Creds.Get(ctx)
}

// Configured reports whether a backup is possible, and what is missing if not.
func (s *Service) Configured(ctx context.Context) (bool, []string, error) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return false, nil, err
	}
	c, err := s.creds(ctx)
	if err != nil {
		c = Credentials{}
	}
	m := st.Missing(c)
	return len(m) == 0, m, nil
}

func (s *Service) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy, s.since = true, s.d.Now()
	return true
}

func (s *Service) release() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *Service) prepare(ctx context.Context, req BackupRequest) (*prepared, error) {
	if !s.acquire() {
		return nil, ErrBusy
	}
	ok := false
	defer func() {
		if !ok {
			s.release()
		}
	}()
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return nil, err
	}
	c, _ := s.creds(ctx)
	if m := st.Missing(c); len(m) > 0 {
		return nil, &NotConfiguredError{Missing: m}
	}
	if err := st.Validate(); err != nil {
		return nil, err
	}
	store, err := s.d.NewStore(st.Target, c)
	if err != nil {
		return nil, err
	}
	rec := RunRecord{StartedAt: s.d.Now().UTC(), Occasion: req.Occasion, Status: "running", Media: req.Media}
	id, err := s.d.Runs.Insert(ctx, rec)
	if err != nil {
		return nil, err
	}
	rec.ID = id
	ok = true
	return &prepared{st: store, settings: st, req: req, runID: id, rec: rec}, nil
}

func (s *Service) execute(ctx context.Context, p *prepared) (Result, error) {
	defer s.release()
	say := p.req.Progress
	if say == nil {
		say = func(string) {}
	}
	say("Backup wird erstellt und verschlüsselt hochgeladen …")
	res, err := Run(ctx, RunOptions{
		Store: p.st, Prefix: p.settings.Prefix, PublicKey: p.settings.PublicKey,
		Occasion: p.req.Occasion, Now: s.d.Now(),
		Build: func(c context.Context, w io.Writer) error { return s.d.Build(c, p.req.Media, w) },
		Progress: func(n int64) {
			say(fmt.Sprintf("%.0f MB hochgeladen …", float64(n)/(1<<20)))
		},
	})
	rec := p.rec
	fin := s.d.Now().UTC()
	rec.FinishedAt = &fin
	if err != nil {
		rec.Status, rec.Error = "failed", err.Error()
		s.record(rec)
		return Result{}, err
	}
	rec.Status, rec.Object, rec.Size = "ok", res.Key, res.Size
	// Retention after a good upload only: never thin out the old backups for a new one
	// that did not arrive. A failed clean-up does not fail the backup.
	if _, perr := Prune(ctx, p.st, p.settings.Prefix, s.d.Now(), s.d.Policy); perr != nil {
		rec.Note = "Aufräumen alter Backups fehlgeschlagen: " + perr.Error()
		log.Printf("offsite: prune: %v", perr)
	}
	s.record(rec)
	say(fmt.Sprintf("Backup gespeichert (%.1f MB).", float64(res.Size)/(1<<20)))
	return res, nil
}

func (s *Service) record(r RunRecord) {
	// Own context: the run's may be the thing that ended.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.d.Runs.Update(ctx, r); err != nil {
		log.Printf("offsite: could not record run %d: %v", r.ID, err)
	}
}

// Backup runs one backup and waits for it.
func (s *Service) Backup(ctx context.Context, req BackupRequest) (Result, error) {
	p, err := s.prepare(ctx, req)
	if err != nil {
		return Result{}, err
	}
	return s.execute(ctx, p)
}

// StartBackup checks everything that can be checked at once, then runs in the background.
func (s *Service) StartBackup(req BackupRequest) error {
	p, err := s.prepare(context.Background(), req)
	if err != nil {
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer cancel()
		if _, err := s.execute(ctx, p); err != nil {
			log.Printf("offsite: backup failed: %v", err)
		}
	}()
	return nil
}

func (s *Service) openStore(ctx context.Context) (Store, Settings, error) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return nil, st, err
	}
	c, _ := s.creds(ctx)
	if m := st.Missing(c); len(m) > 0 {
		// The key is not needed to read; report only what reading needs.
		var need []string
		for _, w := range m {
			if w != "Verschlüsselungsschlüssel" {
				need = append(need, w)
			}
		}
		if len(need) > 0 {
			return nil, st, &NotConfiguredError{Missing: need}
		}
	}
	store, err := s.d.NewStore(st.Target, c)
	return store, st, err
}

// List returns the backups in the bucket, newest first.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	store, st, err := s.openStore(ctx)
	if err != nil {
		return nil, err
	}
	return ListEntries(ctx, store, st.Prefix)
}

// OpenObject streams one backup (encrypted) by file name.
func (s *Service) OpenObject(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	store, st, err := s.openStore(ctx)
	if err != nil {
		return nil, 0, err
	}
	return Open(ctx, store, st.Prefix, name)
}

// Probe tests the saved target.
func (s *Service) Probe(ctx context.Context) (ProbeResult, error) {
	store, st, err := s.openStore(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	return Probe(ctx, store, st.Prefix, s.d.Now()), nil
}

// SettingsInput is a settings change; empty keys keep the stored ones.
type SettingsInput struct {
	Settings
	AccessKey string
	SecretKey string
}

// LoadSettings returns the stored settings and whether keys exist.
func (s *Service) LoadSettings(ctx context.Context) (Settings, bool, error) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return st, false, err
	}
	c, _ := s.creds(ctx)
	return st, c.AccessKey != "" && c.SecretKey != "", nil
}

// SaveSettings validates and stores. The public key is not changed here (own routes), so
// a settings form cannot overwrite it by sending an old value.
func (s *Service) SaveSettings(ctx context.Context, in SettingsInput) error {
	cur, err := s.d.Settings.Load(ctx)
	if err != nil {
		return err
	}
	in.PublicKey = cur.PublicKey
	in.Prefix, err = NormalizePrefix(in.Prefix)
	if err != nil {
		return err
	}
	if err := in.Settings.Validate(); err != nil {
		return err
	}
	if in.AccessKey != "" || in.SecretKey != "" {
		if s.d.Creds == nil {
			return errors.New("Ohne Cluster-Zugriff können die Zugangsschlüssel nicht gespeichert werden.")
		}
		c, _ := s.creds(ctx)
		if in.AccessKey != "" {
			c.AccessKey = in.AccessKey
		}
		if in.SecretKey != "" {
			c.SecretKey = in.SecretKey
		}
		if err := s.d.Creds.Put(ctx, c); err != nil {
			return err
		}
	}
	return s.d.Settings.Save(ctx, in.Settings)
}

// SetPublicKey stores an operator-supplied age public key.
func (s *Service) SetPublicKey(ctx context.Context, key string) error {
	pub, err := ParsePublicKey(key)
	if err != nil {
		return err
	}
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return err
	}
	st.PublicKey = pub
	return s.d.Settings.Save(ctx, st)
}

// ErrKeyExists: generating a pair would replace the key older backups were made with.
var ErrKeyExists = errors.New("Es ist bereits ein Schlüssel eingerichtet. Ein neuer Schlüssel gilt nur für künftige Backups; die alten bleiben nur mit dem alten privaten Schlüssel lesbar.")

// NewKeyPair generates a pair, stores the public half and returns both. The private key
// leaves this function once, to the response, and is stored nowhere.
func (s *Service) NewKeyPair(ctx context.Context, replace bool) (public, private string, err error) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return "", "", err
	}
	if st.PublicKey != "" && !replace {
		return "", "", ErrKeyExists
	}
	public, private, err = GenerateKeyPair()
	if err != nil {
		return "", "", err
	}
	st.PublicKey = public
	if err := s.d.Settings.Save(ctx, st); err != nil {
		return "", "", err
	}
	return public, private, nil
}

// Status is what the backup page shows about automation.
type Status struct {
	Configured      bool        `json:"configured"`
	Missing         []string    `json:"missing,omitempty"`
	Running         bool        `json:"running"`
	RunningSince    *time.Time  `json:"running_since,omitempty"`
	ScheduleEnabled bool        `json:"schedule_enabled"`
	NextRun         *time.Time  `json:"next_run,omitempty"`
	Last            *RunRecord  `json:"last,omitempty"`
	LastOK          *RunRecord  `json:"last_ok,omitempty"`
	Recent          []RunRecord `json:"recent"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	var out Status
	ok, missing, err := s.Configured(ctx)
	if err != nil {
		return out, err
	}
	out.Configured, out.Missing = ok, missing
	st, err := s.d.Settings.Load(ctx)
	if err != nil {
		return out, err
	}
	s.mu.Lock()
	out.Running = s.busy
	if s.busy {
		t := s.since
		out.RunningSince = &t
	}
	s.mu.Unlock()
	out.ScheduleEnabled = st.Schedule
	if st.Schedule && ok {
		if n, err := NextRun(s.d.Now(), st.ScheduleTime); err == nil {
			out.NextRun = &n
		}
	}
	out.Recent, err = s.d.Runs.Recent(ctx, 10)
	if err != nil {
		return out, err
	}
	if len(out.Recent) > 0 {
		out.Last = &out.Recent[0]
	}
	out.LastOK, err = s.d.Runs.LastOK(ctx)
	return out, err
}

// ---- schedule ----

// scheduleWindow is how late after the set time a run still starts. Past it nothing is
// made up: a backup at an unexpected hour, while people are using the server, is not what
// was asked for.
const scheduleWindow = 30 * time.Minute

func slotOn(now time.Time, hhmm string) (time.Time, error) {
	h, m, err := ParseScheduleTime(hhmm)
	if err != nil {
		return time.Time{}, err
	}
	n := now.UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), h, m, 0, 0, time.UTC), nil
}

// Due is the scheduling decision as a pure function: inside the window after today's
// slot, and no attempt (of any outcome) since the slot.
func Due(now time.Time, hhmm string, lastAttempt time.Time) bool {
	slot, err := slotOn(now, hhmm)
	if err != nil {
		return false
	}
	n := now.UTC()
	return !n.Before(slot) && n.Before(slot.Add(scheduleWindow)) && lastAttempt.Before(slot)
}

// NextRun is the next slot after now.
func NextRun(now time.Time, hhmm string) (time.Time, error) {
	slot, err := slotOn(now, hhmm)
	if err != nil {
		return time.Time{}, err
	}
	if !slot.After(now) {
		slot = slot.Add(24 * time.Hour)
	}
	return slot, nil
}

// Tick makes the scheduling decision once and acts on it.
func (s *Service) Tick(ctx context.Context) {
	st, err := s.d.Settings.Load(ctx)
	if err != nil || !st.Schedule {
		return
	}
	last, err := s.d.Runs.LastStart(ctx, OccasionScheduled)
	if err != nil {
		log.Printf("offsite: schedule: %v", err)
		return
	}
	now := s.d.Now()
	if !Due(now, st.ScheduleTime, last) {
		return
	}
	_, err = s.Backup(ctx, BackupRequest{Occasion: OccasionScheduled, Media: st.Media})
	var nc *NotConfiguredError
	if errors.Is(err, ErrBusy) || errors.As(err, &nc) {
		// Recorded, so the page can say why last night has no backup and so the slot
		// counts as attempted: nothing is retried later.
		t := now.UTC()
		_, _ = s.d.Runs.Insert(ctx, RunRecord{StartedAt: t, FinishedAt: &t, Occasion: OccasionScheduled,
			Status: "skipped", Media: st.Media, Error: err.Error()})
		return
	}
	if err != nil {
		log.Printf("offsite: scheduled backup failed: %v", err)
	}
}

// RunSchedule closes runs a restart left open, then checks the clock every 30 seconds.
func (s *Service) RunSchedule(ctx context.Context) {
	if n, err := s.d.Runs.CloseInterrupted(ctx); err != nil {
		log.Printf("offsite: %v", err)
	} else if n > 0 {
		log.Printf("offsite: closed %d backup(s) left running by a restart", n)
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}
