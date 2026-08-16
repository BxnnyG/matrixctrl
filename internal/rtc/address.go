package rtc

import (
	"sort"
	"strings"
	"time"
)

// AddressKey turns a resolver answer into the value that gets recorded.
//
// The whole set, sorted — not one member of it. `net.Resolver.LookupHost` returns
// every A record and rotates their order per query, so recording `addrs[0]` samples
// a coin flip. On this deployment the announced host is proxied and has two A
// records, which produced 889 rows for one address and 889 for the other: 1778
// "changes" in twelve days, none of them real (E45).
//
// Sorting is what makes a rotation stop being a change. Joining is what makes a
// *genuine* change to the set — one address added, another withdrawn — still be one.
func AddressKey(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return ""
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// multiAddress reports whether a recorded key holds more than one address.
func multiAddress(key string) bool { return strings.Contains(key, ",") }

// Freshness answers whether what the SFU announces to clients can still be current.
//
// It never reads the announced address, because LiveKit does not expose it: not on
// its HTTP port, not in its metrics, only in a log line — and a log format is not an
// API to build on. The address is derivable anyway, because the SFU discovers it by
// STUN exactly once, at startup:
//
//	the announcement equals the public address at the moment the pod started,
//	so it is stale exactly when the address changed after that moment.
//
// Both timestamps come from things the product already holds — the pod's start time
// from the API server, and the moment the announced host's DNS answer last changed
// from rtc_address_history.
type Freshness string

const (
	// FreshnessOK — the pod started after the last observed address change.
	FreshnessOK Freshness = "ok"
	// FreshnessStale — the address changed while this pod was already running, so
	// every ICE candidate it offers names an address that no longer routes.
	FreshnessStale Freshness = "stale"
	// FreshnessUnknown — not enough observations, no start time, or no host.
	// A fresh install has witnessed no change and must not read that as "ok":
	// having seen nothing is not evidence of stability.
	FreshnessUnknown Freshness = "unknown"
)

// AddressObservation is the newest row for a host: what it resolved to, and since
// when that answer has been the same.
type AddressObservation struct {
	Address   string    `json:"address"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	// Changes counts how many distinct answers have been recorded for this host.
	// One means nothing has ever changed — which is why FirstSeen alone cannot be
	// treated as "the moment of change".
	Changes int `json:"changes"`
}

// AssessFreshness compares the pod's start with the last address change.
//
// podStart is the SFU pod's start time; a zero value means unknown. obs is the
// newest observation, or nil when the host has never been resolved.
func AssessFreshness(podStart time.Time, obs *AddressObservation) (Freshness, string) {
	if obs == nil {
		return FreshnessUnknown, "the announced host has not been resolved yet"
	}
	if podStart.IsZero() {
		return FreshnessUnknown, "the SFU pod's start time is not available"
	}
	// The premise, checked before the arithmetic that depends on it.
	//
	// This whole comparison rests on the announced host's DNS answer *being* the
	// node's public address — that is what makes "the address changed after the pod
	// started" mean "the address the SFU discovered by STUN is no longer the right
	// one". More than one A record means something sits in front of the node: a home
	// connection has one WAN address, and a set of them is a CDN or a load balancer.
	// Those addresses do not move when the operator's line is reconnected, and they
	// move for reasons that have nothing to do with this deployment.
	//
	// So the answer is Unknown, and the operator is told why. It was previously
	// Stale — permanently, on this deployment, for twelve days, above a button that
	// replaces the SFU pod and drops any call in progress (E45).
	if multiAddress(obs.Address) {
		return FreshnessUnknown, "the announced host resolves to several addresses, so it is behind a CDN or load balancer and its DNS answer is not the node's own public address"
	}

	if obs.Changes < 2 {
		// The first row's first_seen is when MatrixCtrl started watching, not when
		// the address changed. Reporting "ok" from that would be a guess dressed as
		// a fact, and on a fresh install it would be wrong every time the address
		// had in fact changed before we ever looked.
		return FreshnessUnknown, "no address change has been observed yet, so there is nothing to compare against"
	}
	if obs.FirstSeen.After(podStart) {
		return FreshnessStale, "the public address changed after the SFU started, so the address it announces to clients no longer routes"
	}
	return FreshnessOK, "the SFU started after the last address change"
}

// NextObservation decides what to write for a fresh resolve: whether the newest row
// should be extended or a new one started.
//
// Kept as a pure function so the decision is testable without a database, and so the
// SQL stays a transcription of it rather than the place the logic lives.
type ObservationAction int

const (
	// ActionInsert — first ever answer for this host, or the answer changed.
	ActionInsert ObservationAction = iota
	// ActionExtend — same answer as before; only last_seen moves.
	ActionExtend
	// ActionSkip — nothing worth recording (an empty or failed resolution).
	ActionSkip
)

func NextObservation(resolved string, newest *AddressObservation) ObservationAction {
	if resolved == "" {
		// A failed lookup must never be recorded as a change. On an air-gapped
		// cluster every poll would otherwise look like the address moving, and the
		// page would cry stale forever.
		return ActionSkip
	}
	if newest == nil || newest.Address != resolved {
		return ActionInsert
	}
	return ActionExtend
}
