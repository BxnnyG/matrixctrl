package restore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// The rollback is the reason this is a button at all, so it is the thing under test.
//
// It cannot be tested on a cluster: the failure has to happen at a chosen step, and
// every rehearsal costs a homeserver. So Kubernetes and Postgres are two small fakes
// that record what was asked of them, and the assertions are about the *order* of what
// happened — which is where a restore goes wrong, not in the SQL.

type fakePG struct {
	schemaVersion int
	dbs           map[string]bool
	steps         []string
	failOn        map[string]error
}

func newPG(existing ...string) *fakePG {
	f := &fakePG{dbs: map[string]bool{}, failOn: map[string]error{}}
	for _, d := range existing {
		f.dbs[d] = true
	}
	return f
}

func (f *fakePG) note(s string, args ...any) { f.steps = append(f.steps, fmt.Sprintf(s, args...)) }

func (f *fakePG) Exists(_ context.Context, name string) (bool, error) { return f.dbs[name], nil }

func (f *fakePG) Create(_ context.Context, name, owner string) error {
	if err := f.failOn["create"]; err != nil {
		return err
	}
	f.note("create %s owner %s", name, owner)
	f.dbs[name] = true
	return nil
}

func (f *fakePG) Rename(_ context.Context, from, to string) error {
	if err := f.failOn["rename:"+from]; err != nil {
		return err
	}
	if !f.dbs[from] {
		return fmt.Errorf("no such database %s", from)
	}
	f.note("rename %s -> %s", from, to)
	delete(f.dbs, from)
	f.dbs[to] = true
	return nil
}

func (f *fakePG) Drop(_ context.Context, name string) error {
	if err := f.failOn["drop"]; err != nil {
		return err
	}
	f.note("drop %s", name)
	delete(f.dbs, name)
	return nil
}

func (f *fakePG) Terminate(context.Context, string) error { return nil }

func (f *fakePG) Open(_ context.Context, db string) (Sink, error) {
	if err := f.failOn["open"]; err != nil {
		return nil, err
	}
	f.note("open %s", db)
	return &fakeSink{pg: f}, nil
}

type fakeSink struct {
	pg       *fakePG
	loaded   map[string]int64
	mismatch []backup.Mismatch
}

func (s *fakeSink) DeferConstraints(context.Context) error { return nil }

func (s *fakeSink) KeepTargetOnly(context.Context) error {
	s.pg.note("keep")
	return s.pg.failOn["keep"]
}

func (s *fakeSink) MergeKept(context.Context) (int, error) {
	s.pg.note("merge")
	return 2, s.pg.failOn["merge"]
}

// schemaVersion is what the target reports; the archive's comes from the feed.
func (s *fakeSink) SchemaVersion(context.Context) (int, error) {
	return s.pg.schemaVersion, s.pg.failOn["schema"]
}

func (s *fakeSink) TruncateAll(context.Context) (int, error) {
	if err := s.pg.failOn["truncate"]; err != nil {
		return 0, err
	}
	s.pg.note("truncate")
	return 7, nil
}

func (s *fakeSink) LoadCSV(_ context.Context, table string, r io.Reader) (int64, error) {
	if err := s.pg.failOn["load:"+table]; err != nil {
		return 0, err
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	if s.loaded == nil {
		s.loaded = map[string]int64{}
	}
	n := int64(strings.Count(string(body), "\n"))
	s.loaded[table] = n
	s.pg.note("load %s (%d)", table, n)
	return n, nil
}

func (s *fakeSink) SetSequences(_ context.Context, seqs []backup.Sequence) (int, []string, error) {
	if err := s.pg.failOn["sequences"]; err != nil {
		return 0, nil, err
	}
	s.pg.note("sequences %d", len(seqs))
	return len(seqs), nil, nil
}

func (s *fakeSink) Verify(context.Context, map[string]int64) ([]backup.Mismatch, error) {
	s.pg.note("verify")
	return s.mismatch, nil
}

func (s *fakeSink) Close(context.Context) {}

type fakeCluster struct {
	replicas int32
	steps    *[]string
	failOn   map[string]error
}

func (c *fakeCluster) Replicas(context.Context, string, string) (int32, error) {
	return c.replicas, nil
}

func (c *fakeCluster) Scale(_ context.Context, _, name string, n int32) error {
	if err := c.failOn[fmt.Sprintf("scale:%d", n)]; err != nil {
		return err
	}
	*c.steps = append(*c.steps, fmt.Sprintf("scale %s %d", name, n))
	return nil
}

func (c *fakeCluster) WaitGone(context.Context, string) error          { return nil }
func (c *fakeCluster) WaitReady(context.Context, string, string) error { return nil }

func setup(existing ...string) (*fakePG, *fakeCluster, Part, backup.HomeserverManifest) {
	pg := newPG(existing...)
	cl := &fakeCluster{replicas: 1, steps: &pg.steps, failOn: map[string]error{}}
	part := Part{
		Prefix: "homeserver/", Database: "synapse", Owner: "synapse_user",
		Workload: Workload{Kind: "statefulset", Name: "ess-synapse-main", Selector: "app=synapse"},
	}
	man := backup.HomeserverManifest{
		Tables:    []backup.Table{{Name: "events"}, {Name: "rooms"}},
		Sequences: []backup.Sequence{{Name: "events_stream_seq"}},
	}
	return pg, cl, part, man
}

func twoTables(load func(string, io.Reader) error) error {
	if err := load("events", strings.NewReader("a\nb\nc\n")); err != nil {
		return err
	}
	return load("rooms", strings.NewReader("x\n"))
}

func TestDatabaseRestoreSwapsRatherThanOverwrites(t *testing.T) {
	pg, cl, part, man := setup("synapse")

	res, err := Database(context.Background(), part, man, cl, pg, "2026_09_25_1200", nil, twoTables)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	want := []string{
		"scale ess-synapse-main 0",
		"rename synapse -> synapse_vor_2026_09_25_1200",
		"create synapse owner synapse_user",
		"scale ess-synapse-main 1", // the service builds its schema
		"scale ess-synapse-main 0",
		"open synapse",
		"keep",
		"truncate",
		"load events (3)",
		"load rooms (1)",
		"sequences 1",
		"verify",
		"scale ess-synapse-main 1",
	}
	if got := strings.Join(pg.steps, " | "); got != strings.Join(want, " | ") {
		t.Errorf("the order is the design.\n got: %s\nwant: %s", got, strings.Join(want, " | "))
	}
	if res.TotalRows != 4 || res.Tables != 2 {
		t.Errorf("counted %d rows in %d tables, want 4 in 2", res.TotalRows, res.Tables)
	}
	if res.PreviousName != "synapse_vor_2026_09_25_1200" {
		t.Errorf("the operator must be told where the old data went, got %q", res.PreviousName)
	}
	// The old data still exists. Nothing in a successful restore deletes it.
	if !pg.dbs["synapse_vor_2026_09_25_1200"] {
		t.Error("the previous database was not kept")
	}
}

// The property the whole design exists for: a failure partway through leaves the
// operator where they started.
func TestAFailedRestorePutsTheOldDatabaseBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{"a table that will not load", "load:rooms"},
		{"the counters cannot be set", "sequences"},
		{"the empty database cannot be opened", "open"},
		{"the fresh database cannot be emptied", "truncate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg, cl, part, man := setup("synapse")
			pg.failOn[tc.key] = errors.New("boom")

			_, err := Database(context.Background(), part, man, cl, pg, "stamp", nil, twoTables)
			if err == nil {
				t.Fatal("the restore must fail")
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("the cause must survive the rollback: %v", err)
			}
			if !pg.dbs["synapse"] {
				t.Fatal("the database the server needs is gone — this is the outcome the rollback exists to prevent")
			}
			if pg.dbs["synapse_vor_stamp"] {
				t.Error("the old data was left parked under the aside name instead of being put back")
			}
			last := pg.steps[len(pg.steps)-1]
			if last != "scale ess-synapse-main 1" {
				t.Errorf("the service must be running again afterwards, last step was %q", last)
			}
		})
	}
}

// A rollback that cannot complete is the one case where the operator has to act, so the
// message has to carry the statement that puts it right — not a reference to a document.
func TestABlockedRollbackNamesTheWayBack(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	pg.failOn["sequences"] = errors.New("boom")
	pg.failOn["rename:synapse_vor_stamp"] = errors.New("still in use")

	_, err := Database(context.Background(), part, man, cl, pg, "stamp", nil, twoTables)
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, want := range []string{"boom", "synapse_vor_stamp", "ALTER DATABASE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message must contain %q: %v", want, err)
		}
	}
}

// An archive from before etappe 106 has no counters. It still restores — and the
// operator is told, because a silent success is the expensive failure here.
func TestAnArchiveWithoutCountersIsAnnounced(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	man.FormatVersion = 1
	man.Sequences = nil

	log := runWithLog(t, pg, cl, part, man)
	if !saidAbout(log, "sequences", "alten Format") {
		t.Errorf("the missing counters must be said out loud, log was:\n%s", strings.Join(log, "\n"))
	}
}

// …and a database that simply has none must not be reported as an old archive.
//
// The authentication service has exactly zero sequences, which the first rehearsal
// against the live server duly flagged as a defect in a perfectly good archive. A
// warning that fires on a healthy case is how a warning stops being read (§4.96), so
// the statement keys on the archive's format rather than on a count.
func TestADatabaseWithNoCountersIsNotAnOldArchive(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	man.FormatVersion = backup.FormatVersion
	man.Sequences = nil

	log := runWithLog(t, pg, cl, part, man)
	if saidAbout(log, "sequences", "alten Format") {
		t.Errorf("nothing is wrong with this archive, log was:\n%s", strings.Join(log, "\n"))
	}
}

// The service writes rows about itself the moment it starts — which schema it is on,
// which worker processes have registered. Those belong to the installation being
// restored into: loading the archive's copy tells a newer service it is an older one,
// and hands out leases against workers that do not exist.
func TestTablesThatBelongToTheTargetAreNotLoaded(t *testing.T) {
	pg, cl, part, man := setup("synapse")

	_, err := Database(context.Background(), part, man, cl, pg, "stamp", nil,
		func(load func(string, io.Reader) error) error {
			if err := load("events", strings.NewReader("a\n")); err != nil {
				return err
			}
			return load("schema_version", strings.NewReader("99\n"))
		})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if strings.Contains(strings.Join(pg.steps, " "), "load schema_version") {
		t.Errorf("the target's own schema bookkeeping was overwritten: %v", pg.steps)
	}
}

func runWithLog(t *testing.T, pg *fakePG, cl *fakeCluster, part Part, man backup.HomeserverManifest,
	feed ...Feed) []string {
	t.Helper()
	use := Feed(twoTables)
	if len(feed) > 0 {
		use = feed[0]
	}
	var log []string
	prog := Progress(func(step, detail string) { log = append(log, step+": "+detail) })
	if _, err := Database(context.Background(), part, man, cl, pg, "stamp", prog, use); err != nil {
		t.Fatalf("restore: %v", err)
	}
	return log
}

func saidAbout(log []string, step, phrase string) bool {
	for _, l := range log {
		if strings.HasPrefix(l, step+": ") && strings.Contains(l, phrase) {
			return true
		}
	}
	return false
}

// A database built from scratch queues every initial background job. The archive comes
// from a server that did all of it long ago, and keeping the fresh list sets the
// restored server to redoing the lot — which is what the first rehearsal against the
// live homeserver did: users_in_public_rooms went from 7 913 to 0 and the directory was
// rebuilt row by row (§4.106).
func TestQueuedWorkIsDroppedWhenTheSchemaMatches(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	pg.schemaVersion = 94

	log := runWithLog(t, pg, cl, part, man, feedWithSchema(94))
	if strings.Contains(strings.Join(pg.steps, " "), "merge") {
		t.Errorf("nothing needed keeping at the same schema version: %v", pg.steps)
	}
	if !saidAbout(log, "schema", "entfallen") {
		t.Errorf("the decision must be stated: %s", strings.Join(log, "\n"))
	}
}

// …and kept when the target runs something newer, because some of that work belongs to
// deltas the archive has never seen and there is no way to tell which.
func TestQueuedWorkIsKeptWhenTheTargetIsNewer(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	pg.schemaVersion = 96

	log := runWithLog(t, pg, cl, part, man, feedWithSchema(94))
	if !strings.Contains(strings.Join(pg.steps, " "), "merge") {
		t.Errorf("the newer schema's queued work must survive: %v", pg.steps)
	}
	if !saidAbout(log, "schema", "96") {
		t.Errorf("both versions must be named: %s", strings.Join(log, "\n"))
	}
}

// The counting happens before the merge. Otherwise the rows the merge puts back look
// like rows the load did not write — which is exactly how the second rehearsal failed,
// and the rollback it triggered was correct about a restore that was fine.
func TestTheCountIsTakenBeforeAnythingIsAddedBack(t *testing.T) {
	pg, cl, part, man := setup("synapse")
	pg.schemaVersion = 96

	runWithLog(t, pg, cl, part, man, feedWithSchema(94))
	steps := strings.Join(pg.steps, " | ")
	if strings.Index(steps, "merge") < strings.Index(steps, "verify") {
		t.Errorf("verify must come first: %s", steps)
	}
}

func feedWithSchema(version int) Feed {
	return func(load func(string, io.Reader) error) error {
		if err := twoTables(load); err != nil {
			return err
		}
		return load("schema_version", strings.NewReader(
			fmt.Sprintf("version,upgraded\n%d,t\n", version)))
	}
}

// A first run that failed badly leaves an aside database. A second run must not walk
// into it — that would rename the live database on top of the only copy of the old one.
func TestARestoreRefusesToOverwriteAPreviousAside(t *testing.T) {
	pg, cl, part, man := setup("synapse", "synapse_vor_stamp")

	_, err := Database(context.Background(), part, man, cl, pg, "stamp", nil, twoTables)
	if err == nil {
		t.Fatal("a leftover aside database must stop the restore before anything moves")
	}
	if len(pg.steps) != 0 {
		t.Errorf("nothing may happen before that check: %v", pg.steps)
	}
}

// Nothing to rename: a greenfield target has no database yet, and that is a normal
// restore rather than an error.
func TestRestoreIntoAnEmptyServer(t *testing.T) {
	pg, cl, part, man := setup()

	res, err := Database(context.Background(), part, man, cl, pg, "stamp", nil, twoTables)
	if err != nil {
		t.Fatalf("restore into an empty server: %v", err)
	}
	if res.PreviousName != "" {
		t.Errorf("there was nothing to put aside, got %q", res.PreviousName)
	}
	if strings.Contains(strings.Join(pg.steps, " "), "rename") {
		t.Errorf("nothing should have been renamed: %v", pg.steps)
	}
}
