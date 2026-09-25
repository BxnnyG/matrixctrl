// Package restore puts a homeserver back from an archive.
//
// Etappe 106. The archive has been complete since etappe 102 — both databases, the
// uploaded files, the sealed keys — and the restore wrote back only MatrixCtrl's own
// part. The manifest said so honestly, which is not the same as closing the gap.
//
// The dangerous part of a restore is not the loading, it is the moment the old data
// stops being the data. So nothing here writes into a live database:
//
//	the old database is renamed aside, an empty one takes its name, the service builds
//	its schema in it, the rows and counters go in, and only then does anything start
//	again. Every failure before the last step is undone by renaming back.
//
// The two ports below exist so that path can be tested. A rollback that has only ever
// been reasoned about is the kind of assurance this repository keeps finding in its own
// comments (§4.104), and the one place it must not be is here.
package restore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// Progress reports a step as it happens, for the live log the operator watches.
type Progress func(step, detail string)

func (p Progress) say(step, format string, args ...any) {
	if p != nil {
		p(step, fmt.Sprintf(format, args...))
	}
}

// Cluster is everything a restore needs from Kubernetes.
type Cluster interface {
	Replicas(ctx context.Context, kind, name string) (int32, error)
	Scale(ctx context.Context, kind, name string, replicas int32) error
	WaitGone(ctx context.Context, selector string) error
	WaitReady(ctx context.Context, kind, name string) error
}

// Postgres is everything a restore needs from the database server. All of it needs the
// admin role: creating and renaming a database is not something the service accounts
// can do, and deliberately so.
type Postgres interface {
	Exists(ctx context.Context, name string) (bool, error)
	Create(ctx context.Context, name, owner string) error
	Rename(ctx context.Context, from, to string) error
	Drop(ctx context.Context, name string) error
	Terminate(ctx context.Context, name string) error
	Open(ctx context.Context, database string) (Sink, error)
}

// Sink is the connection that gets filled.
type Sink interface {
	DeferConstraints(ctx context.Context) error
	KeepTargetOnly(ctx context.Context) error
	TruncateAll(ctx context.Context) (int, error)
	MergeKept(ctx context.Context) (int, error)
	SchemaVersion(ctx context.Context) (int, error)
	LoadCSV(ctx context.Context, table string, r io.Reader) (int64, error)
	SetSequences(ctx context.Context, seqs []backup.Sequence) (int, []string, error)
	Verify(ctx context.Context, loaded map[string]int64) ([]backup.Mismatch, error)
	Close(ctx context.Context)
}

// Workload is the thing that must be stopped while its database is replaced.
type Workload struct {
	Kind     string // "deployment" or "statefulset"
	Name     string
	Selector string // label selector for the pods, which is what "stopped" is measured on
}

// Part is one database inside an archive.
type Part struct {
	Prefix   string // "homeserver/" or "mas/"
	Database string // "synapse"
	Owner    string // the role that connects to it and owns its schema
	Workload Workload
}

// Result is what happened, in numbers the operator can check.
type Result struct {
	Database string `json:"database"`
	// PreviousName is where the old data now lives; empty if there was none. It is in
	// the response because an operator who has just replaced a homeserver needs to know
	// what to keep and what to remove, and a name they can read is the difference.
	PreviousName string            `json:"previous_name,omitempty"`
	Rows         map[string]int64  `json:"-"`
	Tables       int               `json:"tables"`
	TotalRows    int64             `json:"total_rows"`
	Sequences    int               `json:"sequences"`
	SkippedSeqs  []string          `json:"skipped_sequences,omitempty"`
	Mismatches   []backup.Mismatch `json:"mismatches,omitempty"`
}

// Feed hands the runner one table at a time. The caller owns the archive walk, so the
// upload is read once, in order, and never held in memory.
type Feed func(load func(table string, r io.Reader) error) error

// Database restores one database part.
//
// The order is the whole design:
//
//  1. stop the service          — Postgres refuses to rename a database in use, and
//     that refusal is the guard against a half-swapped server
//  2. rename the live database  — the old data is now safe under a name, untouched
//  3. create an empty one
//  4. start the service         — it builds its own schema, in the version running now,
//     which is what lets an older archive land on a newer ESS
//  5. stop it again             — loading under a running server would race it
//  6. load rows, set counters, verify
//  7. start the service
//
// A failure anywhere in 2–6 rolls back: drop the half-built database, rename the old one
// into place, start the service. The operator ends up where they started, which is the
// property that makes the button pressable at all (§4.88).
func Database(ctx context.Context, p Part, man backup.HomeserverManifest,
	c Cluster, pg Postgres, stamp string, prog Progress, feed Feed) (res Result, err error) {

	res.Database = p.Database
	aside := p.Database + "_vor_" + stamp

	if taken, err := pg.Exists(ctx, aside); err != nil {
		return res, err
	} else if taken {
		return res, fmt.Errorf("%s existiert bereits — ein früherer Durchlauf hat dort etwas abgelegt", aside)
	}

	want, err := c.Replicas(ctx, p.Workload.Kind, p.Workload.Name)
	if err != nil {
		return res, fmt.Errorf("aktuelle Replikate von %s: %w", p.Workload.Name, err)
	}
	if want == 0 {
		// Already down: then it is not this restore's job to decide it should come back
		// up. One is the only sensible guess and a wrong guess here starts a service the
		// operator had stopped on purpose.
		want = 1
		prog.say("stop", "%s stand bereits auf 0 Replikaten", p.Workload.Name)
	}

	stop := func() error {
		if err := c.Scale(ctx, p.Workload.Kind, p.Workload.Name, 0); err != nil {
			return err
		}
		return c.WaitGone(ctx, p.Workload.Selector)
	}
	start := func(n int32) error {
		if err := c.Scale(ctx, p.Workload.Kind, p.Workload.Name, n); err != nil {
			return err
		}
		return c.WaitReady(ctx, p.Workload.Kind, p.Workload.Name)
	}

	prog.say("stop", "%s wird angehalten", p.Workload.Name)
	if err := stop(); err != nil {
		return res, fmt.Errorf("%s anhalten: %w", p.Workload.Name, err)
	}
	if err := pg.Terminate(ctx, p.Database); err != nil {
		return res, fmt.Errorf("offene Verbindungen zu %s schließen: %w", p.Database, err)
	}

	existed, err := pg.Exists(ctx, p.Database)
	if err != nil {
		return res, err
	}
	if existed {
		prog.say("aside", "%s wird zu %s umbenannt — die alten Daten bleiben dort stehen", p.Database, aside)
		if err := pg.Rename(ctx, p.Database, aside); err != nil {
			// Nothing has changed yet; the service goes back up as it was.
			_ = start(want)
			return res, err
		}
		res.PreviousName = aside
	}

	// From here on a failure has to be undone, so every return goes through this.
	rollback := func(cause error) (Result, error) {
		prog.say("rollback", "Fehlschlag — der vorherige Zustand wird wiederhergestellt: %v", cause)
		if err := stop(); err != nil {
			return res, fmt.Errorf("%w — und der Rückweg ist blockiert: %s konnte nicht angehalten werden (%v). "+
				"Die alten Daten liegen unter %s", cause, p.Workload.Name, err, aside)
		}
		_ = pg.Terminate(ctx, p.Database)
		if err := pg.Drop(ctx, p.Database); err != nil {
			return res, fmt.Errorf("%w — und %s konnte nicht entfernt werden (%v). "+
				"Die alten Daten liegen unverändert unter %s und werden mit "+
				"ALTER DATABASE %s RENAME TO %s zurückgeholt", cause, p.Database, err, aside, aside, p.Database)
		}
		if res.PreviousName != "" {
			if err := pg.Rename(ctx, aside, p.Database); err != nil {
				return res, fmt.Errorf("%w — und der Rückweg ist blockiert (%v). "+
					"Die alten Daten liegen unverändert unter %s und werden mit "+
					"ALTER DATABASE %s RENAME TO %s zurückgeholt", cause, err, aside, aside, p.Database)
			}
			res.PreviousName = ""
		}
		if err := start(want); err != nil {
			return res, fmt.Errorf("%w — die Daten sind zurück, %s läuft aber nicht wieder an: %v",
				cause, p.Workload.Name, err)
		}
		prog.say("rollback", "der vorherige Zustand steht wieder")
		return res, cause
	}

	prog.say("create", "leere Datenbank %s wird angelegt", p.Database)
	if err := pg.Create(ctx, p.Database, p.Owner); err != nil {
		return rollback(err)
	}

	// The schema comes from the service, not from the archive (§4.66). It is the only
	// reason the service is started in the middle of a restore, and the comment is here
	// because a future reader will otherwise remove this step as redundant.
	prog.say("schema", "%s baut sein Schema in der leeren Datenbank auf", p.Workload.Name)
	if err := start(want); err != nil {
		return rollback(fmt.Errorf("%s baut das Schema nicht auf: %w", p.Workload.Name, err))
	}
	prog.say("stop", "%s wird für das Einspielen wieder angehalten", p.Workload.Name)
	if err := stop(); err != nil {
		return rollback(err)
	}
	if err := pg.Terminate(ctx, p.Database); err != nil {
		return rollback(err)
	}

	sink, err := pg.Open(ctx, p.Database)
	if err != nil {
		return rollback(err)
	}
	defer sink.Close(ctx)

	if err := sink.DeferConstraints(ctx); err != nil {
		return rollback(err)
	}
	// "Freshly created" is not "empty": building the schema leaves the service's own
	// bookkeeping behind, and a COPY on top of it collides on the primary key. The
	// tables that describe the schema are the ones kept — they belong to the version
	// running here, not to the one the archive came from.
	if err := sink.KeepTargetOnly(ctx); err != nil {
		return rollback(err)
	}
	emptied, err := sink.TruncateAll(ctx)
	if err != nil {
		return rollback(err)
	}
	prog.say("prepare", "%d Tabellen geleert, die Schema-Buchhaltung bleibt stehen", emptied)

	res.Rows = map[string]int64{}
	prog.say("load", "%d Tabellen werden eingespielt", len(man.Tables))
	skipped, archiveSchema := 0, 0
	if err := feed(func(table string, r io.Reader) error {
		// The archive's copy of the schema bookkeeping would tell a newer service that
		// it is an older one. The target's own stays — except for the one number in it
		// that says which version the archive came from, which is read on the way past.
		if backup.BelongsToTheTarget[table] {
			skipped++
			if table == "schema_version" {
				if v, err := backup.SchemaVersionFromCSV(r); err == nil {
					archiveSchema = v
				}
			}
			return nil
		}
		n, err := sink.LoadCSV(ctx, table, r)
		if err != nil {
			return err
		}
		res.Rows[table] = n
		res.Tables++
		res.TotalRows += n
		return nil
	}); err != nil {
		return rollback(err)
	}

	if skipped > 0 {
		prog.say("load", "%d Tabellen der Schema-Buchhaltung übersprungen", skipped)
	}

	set, skippedSeqs, err := sink.SetSequences(ctx, man.Sequences)
	if err != nil {
		return rollback(err)
	}
	res.Sequences, res.SkippedSeqs = set, skippedSeqs
	// Keyed on the archive's format, not on the number of counters. A database can
	// legitimately have none — the authentication service has exactly zero — and the
	// first rehearsal duly warned that its perfectly good archive was missing them.
	// A warning that fires on a healthy case is how a warning stops being read (§4.96).
	if man.FormatVersion < 2 {
		prog.say("sequences", "dieses Archiv ist im alten Format und enthält die Zähler nicht — "+
			"der Server vergibt Nummern neu, die er schon vergeben hat")
	} else {
		prog.say("sequences", "%d Zähler gesetzt", set)
	}

	bad, err := sink.Verify(ctx, res.Rows)
	if err != nil {
		return rollback(err)
	}
	res.Mismatches = bad
	if len(bad) > 0 {
		return rollback(fmt.Errorf("die Datenbank enthält nicht, was geschrieben wurde: %v", bad))
	}
	prog.say("verify", "%d Tabellen, %d Zeilen — gezählt, nicht behauptet", res.Tables, res.TotalRows)

	// Only after the counting, and only when the target is running a newer schema.
	//
	// A database built from scratch queues every initial background job — populate the
	// user directory, build these indexes — because that is what a brand-new homeserver
	// has to do. The archive comes from a server that did all of it long ago. Keeping
	// the fresh list unconditionally sets the restored server to redoing the lot: the
	// first rehearsal against the live server emptied users_in_public_rooms from 7 913
	// to 0 and rebuilt the directory row by row, which converges and for an hour looks
	// exactly like loss (§4.106).
	//
	// When the schema versions match, the archive's list is the truth and the queued
	// work is redundant. When the target is newer, some of those jobs belong to deltas
	// the archive has never seen, and there is no way to tell which — so they are all
	// kept, because a background update is resumable and running one twice is cheap
	// next to never running one that was needed.
	targetSchema, err := sink.SchemaVersion(ctx)
	if err != nil {
		return rollback(err)
	}
	switch {
	case targetSchema == 0 || archiveSchema == 0:
		// No schema version to compare (the authentication service keeps none).
	case targetSchema > archiveSchema:
		kept, err := sink.MergeKept(ctx)
		if err != nil {
			return rollback(err)
		}
		prog.say("schema", "dieses ESS führt Schema %d, das Archiv kam von %d — "+
			"%d Hintergrundaufgaben bleiben offen und laufen nach dem Start", targetSchema, archiveSchema, kept)
	default:
		prog.say("schema", "Schema %d wie im Archiv — die Hintergrundaufgaben eines "+
			"frisch gebauten Servers entfallen, die Daten haben sie hinter sich", targetSchema)
	}

	sink.Close(ctx)
	if err := start(want); err != nil {
		return res, fmt.Errorf("die Daten sind eingespielt, %s läuft aber nicht wieder an: %w. "+
			"Die vorherigen Daten liegen unter %s", p.Workload.Name, err, aside)
	}
	prog.say("done", "%s läuft wieder — die vorherigen Daten liegen unter %s", p.Workload.Name, aside)
	return res, nil
}

// Stamp is the suffix that keeps an aside database findable: sortable, no colons, and
// unambiguous about which run put it there.
func Stamp(t time.Time) string { return t.UTC().Format("2006_01_02_1504") }
