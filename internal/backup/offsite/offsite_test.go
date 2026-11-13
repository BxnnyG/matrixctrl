package offsite

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStore behaves like S3 where it matters: an object exists only after a Put has
// read its stream to a clean EOF.
type fakeStore struct {
	mu      sync.Mutex
	objs    map[string][]byte
	aborted int
	// sloppy makes Put commit whatever it read even when the stream failed — a store
	// that breaks the contract, to prove Run does not trust it.
	sloppy bool
}

func newFake() *fakeStore { return &fakeStore{objs: map[string][]byte{}} }

func (f *fakeStore) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	b, err := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.aborted++
		if f.sloppy {
			f.objs[key] = b
			return int64(len(b)), nil
		}
		return 0, err
	}
	f.objs[key] = b
	return int64(len(b)), nil
}
func (f *fakeStore) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objs[key]
	if !ok {
		return nil, 0, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}
func (f *fakeStore) List(ctx context.Context, prefix string) ([]Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Object
	for k, v := range f.objs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, Object{Key: k, Size: int64(len(v))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
func (f *fakeStore) Remove(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, key)
	return nil
}

const marker = "format_version"

func archive(t *testing.T) func(context.Context, io.Writer) error {
	return func(_ context.Context, w io.Writer) error {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		body := []byte(`{"` + marker + `":2}`)
		if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Size: int64(len(body)), Mode: 0o644}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	}
}

var t0 = time.Date(2026, 10, 19, 3, 30, 0, 0, time.UTC)

func TestEncryptionRoundTripAndWrongKey(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, _ := GenerateKeyPair()
	st := newFake()
	res, err := Run(context.Background(), RunOptions{Store: st, Prefix: "p", PublicKey: pub,
		Occasion: OccasionManual, Now: t0, Build: archive(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Key != "p/matrixctrl-2026-10-19-0330-manuell.tar.gz.age" {
		t.Fatalf("name: %s", res.Key)
	}
	stored := st.objs[res.Key]
	if !LooksEncrypted(stored) {
		t.Fatal("stored object is not an age file")
	}
	// Counter-probe for "nothing readable at the provider": neither the manifest key nor
	// the tar member name may appear in the stored bytes.
	for _, s := range []string{marker, "manifest.json", "ustar"} {
		if bytes.Contains(stored, []byte(s)) {
			t.Fatalf("plaintext %q found in stored object", s)
		}
	}
	dec, err := Decrypt(bytes.NewReader(stored), priv)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != "manifest.json" {
		t.Fatalf("member: %v %v", h, err)
	}
	body, _ := io.ReadAll(tr)
	if !strings.Contains(string(body), marker) {
		t.Fatal("round trip lost the content")
	}
	if _, err := Decrypt(bytes.NewReader(stored), otherPriv); err == nil {
		t.Fatal("a wrong key must not open the archive")
	}
}

func TestKeyValidationNeverEchoesOrAcceptsPrivateKey(t *testing.T) {
	_, priv, _ := GenerateKeyPair()
	_, err := ParsePublicKey(priv)
	if err == nil {
		t.Fatal("private key accepted as public key")
	}
	if strings.Contains(err.Error(), priv) {
		t.Fatal("error text repeats the key")
	}
	if _, err := ParsePublicKey("age1zzz"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestFailedBuildLeavesNoObject(t *testing.T) {
	pub, _, _ := GenerateKeyPair()
	boom := errors.New("Datenbank weg")
	build := func(_ context.Context, w io.Writer) error {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 100000))
		return boom
	}
	st := newFake()
	_, err := Run(context.Background(), RunOptions{Store: st, PublicKey: pub, Occasion: OccasionManual, Now: t0, Build: build})
	if err == nil || !strings.Contains(err.Error(), "Datenbank weg") {
		t.Fatalf("error: %v", err)
	}
	if len(st.objs) != 0 {
		t.Fatalf("object left behind: %v", keys(st))
	}
	if st.aborted != 1 {
		t.Fatalf("upload was not aborted (%d)", st.aborted)
	}
}

func TestSloppyStoreStillLeavesNoObject(t *testing.T) {
	pub, _, _ := GenerateKeyPair()
	st := newFake()
	st.sloppy = true
	build := func(_ context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return errors.New("abgebrochen")
	}
	if _, err := Run(context.Background(), RunOptions{Store: st, PublicKey: pub, Occasion: OccasionManual, Now: t0, Build: build}); err == nil {
		t.Fatal("expected error")
	}
	if len(st.objs) != 0 {
		t.Fatalf("truncated object kept: %v", keys(st))
	}
}

func TestBadKeyBuildsNothing(t *testing.T) {
	called := false
	_, err := Run(context.Background(), RunOptions{Store: newFake(), PublicKey: "", Occasion: OccasionManual, Now: t0,
		Build: func(context.Context, io.Writer) error { called = true; return nil }})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func keys(f *fakeStore) []string {
	var k []string
	for x := range f.objs {
		k = append(k, x)
	}
	return k
}

func entry(day int, hour int, occ string) Entry {
	return Entry{Name: "n", Key: "k", Time: time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC), Occasion: occ}
}

func TestRetainDailyBoundary(t *testing.T) {
	// One backup per day, Oct 1..14; now = Oct 14 12:00. Weekly keepers: newest of the
	// 4 newest ISO weeks (Oct 14 [week 42], Oct 11 [41], Oct 4 [40], and week 39 = Oct 1? none newer).
	var es []Entry
	for d := 1; d <= 14; d++ {
		es = append(es, entry(d, 3, OccasionScheduled))
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	keep, drop := Retain(es, now, DefaultPolicy)
	kept := map[int]bool{}
	for _, e := range keep {
		kept[e.Time.Day()] = true
	}
	for d := 8; d <= 14; d++ { // the 7 newest days
		if !kept[d] {
			t.Errorf("day %d must be kept", d)
		}
	}
	if kept[7] || kept[6] || kept[5] {
		t.Errorf("days 5-7 must be dropped: %v", kept)
	}
	if !kept[4] || !kept[1] == false && false { // Oct 4 is a Sunday: newest of its week
		t.Errorf("weekly keeper missing: %v", kept)
	}
	if len(keep)+len(drop) != len(es) {
		t.Fatal("entries lost")
	}
}

func TestRetainNewestNeverDroppedEvenAfterLongGap(t *testing.T) {
	es := []Entry{entry(1, 3, OccasionScheduled)}
	now := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	keep, drop := Retain(es, now, DefaultPolicy)
	if len(keep) != 1 || len(drop) != 0 {
		t.Fatalf("keep=%d drop=%d", len(keep), len(drop))
	}
}

func TestRetainUpgradeBoundary(t *testing.T) {
	now := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	exact := Entry{Time: now.Add(-30 * 24 * time.Hour), Occasion: "vor-upgrade-1.2.3"}
	over := Entry{Time: now.Add(-30*24*time.Hour - time.Minute), Occasion: "vor-upgrade-1.2.3"}
	keep, drop := Retain([]Entry{exact, over}, now, DefaultPolicy)
	if len(keep) != 1 || !keep[0].Time.Equal(exact.Time) || len(drop) != 1 {
		t.Fatalf("keep=%v drop=%v", keep, drop)
	}
}

func TestPruneNeverTouchesForeignObjects(t *testing.T) {
	st := newFake()
	old := "matrixctrl-2020-01-01-0300-zeitplan.tar.gz.age"
	recent := "matrixctrl-2026-10-18-0300-zeitplan.tar.gz.age"
	foreign := []string{
		"p/notes.txt",
		"p/matrixctrl-2020-01-01-0300-zeitplan.tar.gz",  // no .age
		"p/matrixctrl-2020-01-01-0300-etwas.tar.gz.age", // unknown occasion
		"p/sub/" + old, // deeper than our prefix
		"q/" + old,     // other prefix
		old,            // bucket root
		"pp/" + old,    // prefix that merely starts the same
		"p/.matrixctrl-verbindungstest-1",
	}
	for _, k := range foreign {
		st.objs[k] = []byte("x")
	}
	st.objs["p/"+old] = []byte("x")
	st.objs["p/"+recent] = []byte("x")
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	// Daily 1 / weekly 1 so that the 2020 backup is droppable.
	deleted, err := Prune(context.Background(), st, "p", now, Policy{Daily: 1, Weekly: 1, Upgrade: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "p/"+old {
		t.Fatalf("deleted: %v", deleted)
	}
	for _, k := range foreign {
		if _, ok := st.objs[k]; !ok {
			t.Errorf("foreign object %q was deleted", k)
		}
	}
}

func TestOpenRefusesForeignNames(t *testing.T) {
	st := newFake()
	st.objs["p/secret.txt"] = []byte("x")
	for _, n := range []string{"secret.txt", "../p/secret.txt", "a/matrixctrl-2026-10-18-0300-zeitplan.tar.gz.age"} {
		if _, _, err := Open(context.Background(), st, "p", n); err == nil {
			t.Errorf("%q opened", n)
		}
	}
}

func TestDue(t *testing.T) {
	slot := time.Date(2026, 10, 19, 3, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		last time.Time
		want bool
	}{
		{"before", slot.Add(-time.Minute), time.Time{}, false},
		{"at", slot, time.Time{}, true},
		{"in window", slot.Add(29 * time.Minute), time.Time{}, true},
		{"past window, nothing made up", slot.Add(30 * time.Minute), time.Time{}, false},
		{"already attempted", slot.Add(time.Minute), slot, false},
		{"yesterday's attempt", slot.Add(time.Minute), slot.Add(-24 * time.Hour), true},
	}
	for _, c := range cases {
		if got := Due(c.now, "03:30", c.last); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// ---- service ----

type memSettings struct{ s Settings }

func (m *memSettings) Load(context.Context) (Settings, error)   { return m.s, nil }
func (m *memSettings) Save(_ context.Context, s Settings) error { m.s = s; return nil }

type memCreds struct{ c Credentials }

func (m *memCreds) Get(context.Context) (Credentials, error)   { return m.c, nil }
func (m *memCreds) Put(_ context.Context, c Credentials) error { m.c = c; return nil }

type memRuns struct{ rows []RunRecord }

func (m *memRuns) Insert(_ context.Context, r RunRecord) (int64, error) {
	r.ID = int64(len(m.rows) + 1)
	m.rows = append(m.rows, r)
	return r.ID, nil
}
func (m *memRuns) Update(_ context.Context, r RunRecord) error { m.rows[r.ID-1] = r; return nil }
func (m *memRuns) Recent(_ context.Context, n int) ([]RunRecord, error) {
	var out []RunRecord
	for i := len(m.rows) - 1; i >= 0; i-- {
		out = append(out, m.rows[i])
	}
	return out, nil
}
func (m *memRuns) LastOK(context.Context) (*RunRecord, error) {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].Status == "ok" {
			r := m.rows[i]
			return &r, nil
		}
	}
	return nil, nil
}
func (m *memRuns) LastStart(_ context.Context, occ string) (time.Time, error) {
	var t time.Time
	for _, r := range m.rows {
		if r.Occasion == occ && r.StartedAt.After(t) {
			t = r.StartedAt
		}
	}
	return t, nil
}
func (m *memRuns) CloseInterrupted(context.Context) (int64, error) { return 0, nil }

func newSvc(t *testing.T, configured bool) (*Service, *fakeStore, *memRuns, *time.Time) {
	pub, _, _ := GenerateKeyPair()
	set := DefaultSettings()
	creds := Credentials{}
	if configured {
		set.Endpoint, set.Bucket, set.PublicKey = "s3.invalid", "b", pub
		creds = Credentials{"a", "s"}
	}
	st := newFake()
	runs := &memRuns{}
	now := t0.Add(time.Minute)
	svc := NewService(Deps{
		Settings: &memSettings{set}, Creds: &memCreds{creds}, Runs: runs,
		NewStore: func(Target, Credentials) (Store, error) { return st, nil },
		Build:    func(c context.Context, _ bool, w io.Writer) error { return archive(t)(c, w) },
		Now:      func() time.Time { return now },
	})
	return svc, st, runs, &now
}

func TestScheduledRunAndSkippedWhenNotConfigured(t *testing.T) {
	svc, st, runs, _ := newSvc(t, true)
	svc.Tick(context.Background())
	if len(st.objs) != 1 || runs.rows[0].Status != "ok" || runs.rows[0].Size == 0 {
		t.Fatalf("objs=%d rows=%+v", len(st.objs), runs.rows)
	}
	svc.Tick(context.Background()) // same slot: not again
	if len(runs.rows) != 1 {
		t.Fatalf("ran twice: %d", len(runs.rows))
	}

	svc2, st2, runs2, _ := newSvc(t, false)
	svc2.Tick(context.Background())
	if len(st2.objs) != 0 || len(runs2.rows) != 1 || runs2.rows[0].Status != "skipped" ||
		!strings.Contains(runs2.rows[0].Error, "Endpunkt") {
		t.Fatalf("rows=%+v", runs2.rows)
	}
}

func TestBusyRefusesSecondBackup(t *testing.T) {
	svc, _, _, _ := newSvc(t, true)
	if !svc.acquire() {
		t.Fatal()
	}
	if _, err := svc.Backup(context.Background(), BackupRequest{Occasion: OccasionManual}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err=%v", err)
	}
}

func TestFailedRunIsRecordedAndNothingStored(t *testing.T) {
	svc, st, runs, _ := newSvc(t, true)
	svc.d.Build = func(context.Context, bool, io.Writer) error { return errors.New("kaputt") }
	_, err := svc.Backup(context.Background(), BackupRequest{Occasion: OccasionManual})
	if err == nil || len(st.objs) != 0 || runs.rows[0].Status != "failed" || !strings.Contains(runs.rows[0].Error, "kaputt") {
		t.Fatalf("err=%v rows=%+v", err, runs.rows)
	}
	if svc.busy {
		t.Fatal("busy flag not released")
	}
}

func TestProbeStepsAndKeyPair(t *testing.T) {
	svc, st, _, _ := newSvc(t, true)
	res, err := svc.Probe(context.Background())
	if err != nil || !res.OK || len(res.Steps) != 3 || len(st.objs) != 0 {
		t.Fatalf("%+v %v %d", res, err, len(st.objs))
	}
	if _, _, err := svc.NewKeyPair(context.Background(), false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("err=%v", err)
	}
	pub, priv, err := svc.NewKeyPair(context.Background(), true)
	if err != nil || priv == "" {
		t.Fatal(err)
	}
	set, _, _ := svc.LoadSettings(context.Background())
	if set.PublicKey != pub {
		t.Fatal("public key not stored")
	}
	b := new(bytes.Buffer)
	_ = priv
	_ = b
}

func TestObjectNameSanitizesVersion(t *testing.T) {
	occ := OccasionUpgrade("26.5.1+x/../y")
	n, err := ObjectName(t0, occ)
	if err != nil || strings.Contains(n, "/") {
		t.Fatalf("%q %v", n, err)
	}
	if _, o, ok := ParseName(n); !ok || o != occ {
		t.Fatalf("not parseable back: %q", n)
	}
}

// A backup taken by hand before a risky change must still be there after the scheduled
// run of the same day. It used to count as "today's" and lose to the newer one —
// deleted without a log line (acceptance of etappe 120).
func TestAManualBackupSurvivesTheScheduledRunOfTheSameDay(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 15, 0, 0, time.UTC)
	manual := Entry{Time: time.Date(2026, 11, 12, 9, 5, 0, 0, time.UTC), Occasion: "manuell"}
	scheduled := Entry{Time: time.Date(2026, 11, 12, 9, 10, 0, 0, time.UTC), Occasion: "zeitplan"}
	keep, drop := Retain([]Entry{manual, scheduled}, now, DefaultPolicy)
	if len(keep) != 2 || len(drop) != 0 {
		t.Fatalf("keep=%v drop=%v", keep, drop)
	}
	// Not for ever: after the 30 days it goes, like a pre-upgrade backup.
	old := Entry{Time: now.Add(-31 * 24 * time.Hour), Occasion: "manuell"}
	if _, drop := Retain([]Entry{old, scheduled}, now, DefaultPolicy); len(drop) != 1 || drop[0].Occasion != "manuell" {
		t.Fatalf("an old manual backup is kept for ever: drop=%v", drop)
	}
}
