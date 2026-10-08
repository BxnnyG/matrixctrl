package synmetrics

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T) Snapshot {
	t.Helper()
	f, err := os.Open("testdata/synapse-1.158-main.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(f, time.Unix(1790800000, 0))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The fixture is a real Synapse 1.158 main process, cut to the families read here.
func TestParsesARealSynapse(t *testing.T) {
	s := load(t)
	if s.CPU < 1000 || s.StartTime < 1.7e9 || s.Memory < 1e8 {
		t.Fatalf("process counters: cpu=%v start=%v mem=%v", s.CPU, s.StartTime, s.Memory)
	}
	if s.Servlets["HealthResource"] <= 0 {
		t.Errorf("servlet CPU missing: %v", s.Servlets)
	}
	if len(s.Background) < 10 {
		t.Errorf("background jobs: %d", len(s.Background))
	}
}

func TestSomethingElseIsNotASynapse(t *testing.T) {
	if _, err := Parse(strings.NewReader("# TYPE up gauge\nup 1\n"), time.Now()); err == nil {
		t.Fatal("a metrics page without process counters was accepted")
	}
}

func snap(at int64, start, cpu float64, servlets map[string]float64) Snapshot {
	return Snapshot{At: time.Unix(at, 0), StartTime: start, CPU: cpu, Servlets: servlets, Background: map[string]float64{}}
}

func TestARateIsCoresAndSplitsByArea(t *testing.T) {
	a := snap(1000, 500, 100, map[string]float64{"SyncRestServlet": 10, "FederationSendServlet": 5})
	b := snap(1060, 500, 130, map[string]float64{"SyncRestServlet": 22, "FederationSendServlet": 11})
	r, ok := Between(a, b)
	if !ok {
		t.Fatal("refused a valid pair")
	}
	if math.Abs(r.Cores-0.5) > 1e-9 {
		t.Errorf("cores = %v, want 0.5 (30 s of CPU in 60 s)", r.Cores)
	}
	if math.Abs(r.Areas["sync"]-0.2) > 1e-9 || math.Abs(r.Areas["federation-in"]-0.1) > 1e-9 {
		t.Errorf("areas = %v", r.Areas)
	}
}

// A restart resets every counter. Differenced naively, the next minute reads as a large
// negative load — an idle process exactly when it just crashed.
func TestARestartIsNotARate(t *testing.T) {
	before := snap(1000, 500, 9000, nil)
	after := snap(1060, 1055, 3, nil)
	if _, ok := Between(before, after); ok {
		t.Fatal("a pair across a restart produced a rate")
	}
	if _, ok := Between(after, before); ok {
		t.Fatal("readings out of order produced a rate")
	}
}

func TestLifetimeNeedsAStartTime(t *testing.T) {
	s := snap(1_000_000, 0, 100, nil)
	if _, ok := Lifetime(s); ok {
		t.Fatal("a lifetime average without a start time")
	}
	s.StartTime = 1_000_000 - 1000
	r, ok := Lifetime(s)
	if !ok || math.Abs(r.Cores-0.1) > 1e-9 {
		t.Fatalf("lifetime = %+v", r)
	}
}

// The names are the ones production reported on 2026-10-08; each must land in the area
// whose worker would actually take it over.
func TestServletsLandInTheirAreas(t *testing.T) {
	for servlet, want := range map[string]string{
		"FederationSendServlet":             "federation-in",
		"FederationUserDevicesQueryServlet": "federation-read",
		"FederationClientKeysClaimServlet":  "federation-read",
		"FederationV2SendJoinServlet":       "federation-read",
		"SyncRestServlet":                   "sync",
		"SlidingSyncRestServlet":            "sliding-sync",
		"ThumbnailResource":                 "media",
		"RoomSendEventRestServlet":          "send",
		"RoomMessageListRestServlet":        "read",
		"KeyQueryServlet":                   "keys",
		"KeyUploadServlet":                  "keys",
		"HealthResource":                    "health",
		"ListDestinationsRestServlet":       "other",
	} {
		if got := ServletArea(servlet); got != want {
			t.Errorf("ServletArea(%q) = %q, want %q", servlet, got, want)
		}
	}
	for job, want := range map[string]string{
		"federation_transaction_transmission_loop": "federation-out",
		"wake_destinations_needing_catchup":        "federation-out",
		"_get_stats_for_federation_staging":        "federation-in",
		"expire_url_cache_data":                    "media",
		"update_recently_accessed_media":           "media",
		"rotate_notifs":                            "background",
	} {
		if got := BackgroundArea(job); got != want {
			t.Errorf("BackgroundArea(%q) = %q, want %q", job, got, want)
		}
	}
}

func TestEveryAreaWithAWorkerNamesAnESSWorkerType(t *testing.T) {
	ess := map[string]bool{"account-data": true, "appservice": true, "background": true, "client-reader": true,
		"device-lists": true, "encryption": true, "event-creator": true, "event-persister": true,
		"federation-inbound": true, "federation-reader": true, "federation-sender": true,
		"initial-synchrotron": true, "mas-helper": true, "media-repository": true, "presence-writer": true,
		"push-rules": true, "pusher": true, "receipts": true, "sliding-sync": true, "sso-login": true,
		"synchrotron": true, "typing-persister": true, "user-dir": true}
	for _, a := range Areas {
		if a.Worker != "" && !ess[a.Worker] {
			t.Errorf("area %s names worker %q, which matrix-stack 26.9.3 does not have", a.ID, a.Worker)
		}
	}
}
