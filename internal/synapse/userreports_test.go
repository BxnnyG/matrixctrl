package synapse

import (
	"net/url"
	"testing"
)

// The rename is the whole point of the wire type: both fields are plain user ids, so
// swapping them renders the reporter as the offender and nothing about the value
// itself would look wrong.
func TestUserReportWireRenamesBothIDs(t *testing.T) {
	w := userReportWire{
		ID: 7, ReceivedTS: 1700000000000,
		UserID:       "@reporter:example.org",
		TargetUserID: "@target:example.org",
		Reason:       "spam",
	}
	got := w.toUserReport()
	if got.Reporter != "@reporter:example.org" {
		t.Errorf("Reporter = %q, want the user_id field", got.Reporter)
	}
	if got.Target != "@target:example.org" {
		t.Errorf("Target = %q, want the target_user_id field", got.Target)
	}
	if got.ID != 7 || got.Reason != "spam" || got.ReceivedTS != 1700000000000 {
		t.Errorf("carried fields wrong: %+v", got)
	}
}

func TestUserReportOptionsQuery(t *testing.T) {
	cases := []struct {
		name string
		in   UserReportOptions
		want map[string]string
	}{
		{
			// A moderation queue is read from the top, so an unset Dir must not
			// inherit Synapse's default silently.
			name: "defaults",
			in:   UserReportOptions{},
			want: map[string]string{"limit": "50", "dir": "b"},
		},
		{
			name: "searches map onto Synapse's parameter names",
			in:   UserReportOptions{ReporterSearch: "@a:x", TargetSearch: "@b:x"},
			want: map[string]string{"user_id": "@a:x", "target_user_id": "@b:x"},
		},
		{
			// Synapse rejects a negative limit and caps nothing; an oversized page is
			// clamped here rather than turned into a 400 the operator has to read.
			name: "limit is clamped",
			in:   UserReportOptions{Limit: 5000},
			want: map[string]string{"limit": "50"},
		},
		{
			name: "forward direction is passed through",
			in:   UserReportOptions{Dir: "f", From: 20},
			want: map[string]string{"dir": "f", "from": "20"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := url.ParseQuery(c.in.query())
			if err != nil {
				t.Fatalf("query does not parse: %v", err)
			}
			for k, want := range c.want {
				if got := v.Get(k); got != want {
					t.Errorf("%s = %q, want %q (full: %s)", k, got, want, c.in.query())
				}
			}
		})
	}
}

// `from` is an offset, and sending `from=0` explicitly is harmless but noisy; the
// event queue omits it and these two must page the same way.
func TestUserReportOptionsOmitsZeroFrom(t *testing.T) {
	v, _ := url.ParseQuery(UserReportOptions{}.query())
	if _, ok := v["from"]; ok {
		t.Error("from should be omitted when zero")
	}
}
