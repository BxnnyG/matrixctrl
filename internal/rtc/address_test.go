package rtc

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The production failure, twice over: the pod started, the line was re-addressed
// overnight, and every ICE candidate it offered named an address that no longer
// routed.
func TestAddressChangedAfterPodStartIsStale(t *testing.T) {
	pod := at("2026-08-02T17:59:38Z")
	obs := &AddressObservation{
		Address: "203.0.113.7", FirstSeen: at("2026-08-03T03:14:00Z"),
		LastSeen: at("2026-08-03T16:00:00Z"), Changes: 4,
	}

	got, why := AssessFreshness(pod, obs)
	if got != FreshnessStale {
		t.Fatalf("expected stale, got %s (%s)", got, why)
	}
	if why == "" {
		t.Fatal("a stale finding without a reason is not actionable")
	}
}

func TestPodStartedAfterTheChangeIsOK(t *testing.T) {
	obs := &AddressObservation{
		Address: "203.0.113.7", FirstSeen: at("2026-08-03T03:14:00Z"),
		LastSeen: at("2026-08-03T16:00:00Z"), Changes: 4,
	}
	if got, why := AssessFreshness(at("2026-08-03T16:05:00Z"), obs); got != FreshnessOK {
		t.Fatalf("expected ok, got %s (%s)", got, why)
	}
}

// A fresh install has watched one address and seen it change zero times. Its
// first_seen is when MatrixCtrl started looking, not when anything moved — so
// comparing against it would answer a question nobody asked.
func TestSingleObservationIsUnknownNotOK(t *testing.T) {
	obs := &AddressObservation{
		Address: "203.0.113.7", FirstSeen: at("2026-08-03T12:00:00Z"),
		LastSeen: at("2026-08-03T16:00:00Z"), Changes: 1,
	}
	// The pod started before first_seen, which under a naive comparison would look
	// exactly like the stale case.
	got, why := AssessFreshness(at("2026-08-03T09:00:00Z"), obs)
	if got != FreshnessUnknown {
		t.Fatalf("one observation must be unknown, got %s (%s)", got, why)
	}
}

func TestNoObservationIsUnknown(t *testing.T) {
	if got, _ := AssessFreshness(at("2026-08-03T09:00:00Z"), nil); got != FreshnessUnknown {
		t.Fatalf("expected unknown, got %s", got)
	}
}

func TestNoPodStartIsUnknown(t *testing.T) {
	obs := &AddressObservation{Address: "203.0.113.7", FirstSeen: at("2026-08-03T03:14:00Z"), Changes: 3}
	if got, _ := AssessFreshness(time.Time{}, obs); got != FreshnessUnknown {
		t.Fatalf("a missing pod start must be unknown, got %s", got)
	}
}

// A failed lookup must not be recorded. On an air-gapped cluster every poll would
// otherwise look like the address moving, and the page would report stale forever —
// the same "absence read as a signal" mistake in a new place.
func TestFailedResolutionIsNotAChange(t *testing.T) {
	newest := &AddressObservation{Address: "203.0.113.7", Changes: 1}
	if got := NextObservation("", newest); got != ActionSkip {
		t.Fatalf("an empty resolution must be skipped, got %v", got)
	}
	if got := NextObservation("", nil); got != ActionSkip {
		t.Fatalf("an empty resolution with no history must be skipped, got %v", got)
	}
}

func TestSameAddressExtendsRatherThanInserting(t *testing.T) {
	newest := &AddressObservation{Address: "203.0.113.7", Changes: 2}
	if got := NextObservation("203.0.113.7", newest); got != ActionExtend {
		t.Fatalf("an unchanged answer must extend, got %v", got)
	}
}

func TestChangedAddressInserts(t *testing.T) {
	newest := &AddressObservation{Address: "203.0.113.7", Changes: 2}
	if got := NextObservation("203.0.113.9", newest); got != ActionInsert {
		t.Fatalf("a changed answer must insert, got %v", got)
	}
}

func TestFirstEverAnswerInserts(t *testing.T) {
	if got := NextObservation("203.0.113.7", nil); got != ActionInsert {
		t.Fatalf("the first answer must insert, got %v", got)
	}
}

// Every state must carry a reason. A page that says "stale" without saying why
// sends the operator back to the router, which is where this whole class of problem
// has already cost them an evening.
func TestEveryVerdictExplainsItself(t *testing.T) {
	cases := []struct {
		name string
		pod  time.Time
		obs  *AddressObservation
	}{
		{"stale", at("2026-08-02T17:00:00Z"), &AddressObservation{FirstSeen: at("2026-08-03T03:00:00Z"), Changes: 3}},
		{"ok", at("2026-08-03T04:00:00Z"), &AddressObservation{FirstSeen: at("2026-08-03T03:00:00Z"), Changes: 3}},
		{"single observation", at("2026-08-03T04:00:00Z"), &AddressObservation{FirstSeen: at("2026-08-03T03:00:00Z"), Changes: 1}},
		{"no observation", at("2026-08-03T04:00:00Z"), nil},
		{"no pod start", time.Time{}, &AddressObservation{FirstSeen: at("2026-08-03T03:00:00Z"), Changes: 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, why := AssessFreshness(c.pod, c.obs); why == "" {
				t.Fatal("verdict carries no reason")
			}
		})
	}
}

// The bug this etappe exists for: the resolver rotates a multi-record answer, so
// recording one member produced 1778 rows in twelve days — 889 for each of two
// Cloudflare addresses — and a permanently false staleness warning on top of them.
func TestAddressKeyIsOrderIndependent(t *testing.T) {
	a := AddressKey([]string{"104.21.33.5", "172.67.188.105"})
	b := AddressKey([]string{"172.67.188.105", "104.21.33.5"})
	if a != b {
		t.Errorf("rotation changed the key: %q vs %q", a, b)
	}
	// And the rotation must not read as a change at the layer that decides.
	obs := &AddressObservation{Address: a}
	if got := NextObservation(b, obs); got != ActionExtend {
		t.Errorf("NextObservation on a rotated answer = %v, want ActionExtend", got)
	}
}

// A genuine change to the set is still a change. This is the half that a naive
// "just take the sorted first element" fix would have broken.
func TestAddressKeyStillSeesRealChanges(t *testing.T) {
	before := AddressKey([]string{"104.21.33.5", "172.67.188.105"})
	after := AddressKey([]string{"104.21.33.5", "198.51.100.7"})
	if before == after {
		t.Fatal("a withdrawn address is a change")
	}
	if got := NextObservation(after, &AddressObservation{Address: before}); got != ActionInsert {
		t.Errorf("NextObservation on a changed set = %v, want ActionInsert", got)
	}
}

func TestAddressKeyEdges(t *testing.T) {
	if got := AddressKey(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := AddressKey([]string{"", "  "}); got != "" {
		t.Errorf("blanks = %q, want empty — and empty must reach ActionSkip", got)
	}
	if got := AddressKey([]string{"203.0.113.9"}); got != "203.0.113.9" {
		t.Errorf("single = %q", got)
	}
}

// The second cause, and the more important one: even recorded perfectly, a CDN's
// anycast addresses are not the node's public address, so the comparison cannot
// answer the question. Unknown with a reason, never stale.
func TestMultipleAddressesRefuseTheVerdict(t *testing.T) {
	podStart := time.Date(2026, 8, 16, 0, 49, 53, 0, time.UTC)
	obs := &AddressObservation{
		Address:   AddressKey([]string{"104.21.33.5", "172.67.188.105"}),
		FirstSeen: podStart.Add(14 * time.Hour), // long after the pod started
		Changes:   1778,
	}
	got, why := AssessFreshness(podStart, obs)
	if got != FreshnessUnknown {
		t.Errorf("verdict = %q, want %q — a proxied host cannot answer this", got, FreshnessUnknown)
	}
	if why == "" {
		t.Error("Unknown without a reason is the thing this product refuses to ship")
	}
}

// A single-address host still gets a real verdict — the fix must not disable the
// check it was built to make honest.
func TestSingleAddressStillAssessed(t *testing.T) {
	podStart := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	stale := &AddressObservation{Address: "203.0.113.9", FirstSeen: podStart.Add(time.Hour), Changes: 3}
	if got, _ := AssessFreshness(podStart, stale); got != FreshnessStale {
		t.Errorf("changed after pod start = %q, want %q", got, FreshnessStale)
	}
	ok := &AddressObservation{Address: "203.0.113.9", FirstSeen: podStart.Add(-time.Hour), Changes: 3}
	if got, _ := AssessFreshness(podStart, ok); got != FreshnessOK {
		t.Errorf("changed before pod start = %q, want %q", got, FreshnessOK)
	}
}
