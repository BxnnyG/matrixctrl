package auth

import (
	"sync"
	"testing"
	"time"
)

// The whole point: a ticket found in a log is worth nothing after the connection it
// opened.
func TestATicketWorksExactlyOnce(t *testing.T) {
	w := NewWSTickets()
	id, err := w.Issue("user-1")
	if err != nil {
		t.Fatal(err)
	}

	got, ok := w.Redeem(id)
	if !ok || got != "user-1" {
		t.Fatalf("first redemption should succeed, got %q ok=%v", got, ok)
	}
	if _, ok := w.Redeem(id); ok {
		t.Fatal("a replayed ticket was accepted — this is the attack the etappe exists to stop")
	}
}

func TestExpiredTicketIsRefused(t *testing.T) {
	w := NewWSTickets()
	now := time.Now()
	w.now = func() time.Time { return now }

	id, err := w.Issue("user-1")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(ticketTTL + time.Second)
	if _, ok := w.Redeem(id); ok {
		t.Fatal("an expired ticket was accepted")
	}
}

// An expired ticket must be consumed too, so that a clock moving backwards — or a
// test, or a leap second — cannot make a spent ticket usable again.
func TestExpiredTicketIsAlsoConsumed(t *testing.T) {
	w := NewWSTickets()
	now := time.Now()
	w.now = func() time.Time { return now }

	id, _ := w.Issue("user-1")
	now = now.Add(ticketTTL + time.Second)
	w.Redeem(id) // refused, but spent

	now = now.Add(-2 * ticketTTL) // clock goes backwards; the ticket is "valid" again
	if _, ok := w.Redeem(id); ok {
		t.Fatal("a ticket survived its own refusal")
	}
}

func TestUnknownAndEmptyTicketsAreRefused(t *testing.T) {
	w := NewWSTickets()
	if _, ok := w.Redeem("never-issued"); ok {
		t.Error("an unknown ticket was accepted")
	}
	if _, ok := w.Redeem(""); ok {
		t.Error("an empty ticket was accepted")
	}
}

func TestTicketsAreBoundToTheirUser(t *testing.T) {
	w := NewWSTickets()
	a, _ := w.Issue("alice")
	b, _ := w.Issue("bob")

	if got, _ := w.Redeem(a); got != "alice" {
		t.Errorf("expected alice, got %q", got)
	}
	if got, _ := w.Redeem(b); got != "bob" {
		t.Errorf("expected bob, got %q", got)
	}
}

func TestTicketsAreUnique(t *testing.T) {
	w := NewWSTickets()
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := w.Issue("user-1")
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatal("issued a duplicate ticket")
		}
		seen[id] = true
	}
}

// An authenticated client looping on the issue endpoint must not grow the process
// without limit.
func TestStoreIsBounded(t *testing.T) {
	w := NewWSTickets()
	var lastErr error
	for i := 0; i < maxTickets+50; i++ {
		if _, err := w.Issue("user-1"); err != nil {
			lastErr = err
			break
		}
	}
	if lastErr == nil {
		t.Fatal("the store accepted more than its bound")
	}

	w.mu.Lock()
	size := len(w.tickets)
	w.mu.Unlock()
	if size > maxTickets {
		t.Fatalf("store grew past the bound: %d", size)
	}
}

// Expired tickets must not wedge the store shut once it is full.
func TestExpiredTicketsAreSweptSoIssuingRecovers(t *testing.T) {
	w := NewWSTickets()
	now := time.Now()
	w.now = func() time.Time { return now }

	for i := 0; i < maxTickets; i++ {
		if _, err := w.Issue("user-1"); err != nil {
			t.Fatalf("filling the store failed at %d: %v", i, err)
		}
	}
	if _, err := w.Issue("user-1"); err == nil {
		t.Fatal("expected the store to be full")
	}

	now = now.Add(ticketTTL + time.Second)
	if _, err := w.Issue("user-1"); err != nil {
		t.Fatalf("a full store of expired tickets should sweep and accept: %v", err)
	}
}

func TestIssueRefusesAnEmptyUser(t *testing.T) {
	w := NewWSTickets()
	if _, err := w.Issue(""); err == nil {
		t.Fatal("a ticket belonging to nobody would authenticate as nobody")
	}
}

// The handler issues and the handshake redeems, on different connections.
func TestConcurrentUseIsSafe(t *testing.T) {
	w := NewWSTickets()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if id, err := w.Issue("user-1"); err == nil {
				w.Redeem(id)
			}
		}()
	}
	wg.Wait()
}
