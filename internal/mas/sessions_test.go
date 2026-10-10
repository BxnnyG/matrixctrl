package mas

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(-d); return &t }

const ours = "01HCLIENT0000000000000000A"

func TestOnlyWhatMatrixCtrlLeftBehindIsPicked(t *testing.T) {
	grant := "openid urn:matrix:org.matrix.msc2967.client:api:* urn:synapse:admin:*"
	const u = "01HUSER00000000000000000AA"
	sessions := []OAuth2Session{
		{ID: "login-old", ClientID: ours, UserID: u, Scope: "openid email", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "login-now", ClientID: ours, UserID: u, Scope: "openid email", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "grant-idle", ClientID: ours, UserID: u, Scope: grant, CreatedAt: now.Add(-72 * time.Hour), LastActiveAt: at(30 * time.Hour)},
		{ID: "grant-used", ClientID: ours, UserID: u, Scope: grant, CreatedAt: now.Add(-72 * time.Hour), LastActiveAt: at(4 * time.Minute)},
		{ID: "grant-fresh", ClientID: ours, UserID: u, Scope: grant, CreatedAt: now.Add(-1 * time.Hour)},
		// MatrixCtrl's own admin access: one session per minted token, no user.
		{ID: "admin-old", ClientID: ours, Scope: "urn:mas:admin", CreatedAt: now.Add(-5 * time.Hour), LastActiveAt: at(4 * time.Hour)},
		{ID: "admin-current", ClientID: ours, Scope: "urn:mas:admin", CreatedAt: now.Add(-3 * time.Minute), LastActiveAt: at(10 * time.Second)},
		// Element, the operator's phone: never MatrixCtrl's to end.
		{ID: "element", ClientID: "01ELEMENTCLIENT0000000000", Scope: grant, CreatedAt: now.Add(-900 * time.Hour)},
		{ID: "done", ClientID: ours, UserID: u, Scope: "openid email", CreatedAt: now.Add(-9 * time.Hour), FinishedAt: at(8 * time.Hour)},
	}
	got := map[string]bool{}
	for _, s := range StaleSessions(sessions, ours, now) {
		got[s.ID] = true
	}
	want := map[string]bool{"login-old": true, "grant-idle": true, "admin-old": true}
	if len(got) != len(want) {
		t.Fatalf("picked %v, want %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("missed %s", id)
		}
	}
}

func TestSessionsParseFromMASJSONAPI(t *testing.T) {
	raw := []byte(`{"data":[{"type":"oauth2-session","id":"01J0000000000000000000000A","attributes":{
		"created_at":"2026-08-06T10:55:00Z","finished_at":null,"user_id":"01KUSER000000000000000000A",
		"user_session_id":null,"client_id":"01HCLIENT0000000000000000A","scope":"openid email",
		"user_agent":null,"last_active_at":"2026-08-06T10:55:01Z","last_active_ip":null,"human_name":null}}]}`)
	got, err := parseSessions(raw)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	s := got[0]
	if s.ClientID != ours || s.Scope != "openid email" || s.UserID == "" || s.LastActiveAt == nil || s.FinishedAt != nil {
		t.Errorf("parsed %+v", s)
	}
}
