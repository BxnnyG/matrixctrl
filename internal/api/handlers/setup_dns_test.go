package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The record list and the deploy must never drift apart.
//
// The deploy writes greenfieldHostnames into the config; the DNS step tells the
// operator which names to create. If a hostname is ever added to one and not the
// other, an operator creates the records they were shown, deploys, and one service is
// unreachable for a reason nothing on screen mentions. Deriving the list is what makes
// that impossible; this test is what keeps the derivation honest.
func TestEveryDeployedHostnameHasARecord(t *testing.T) {
	const sn = "example.com"
	records := greenfieldRecords(sn)

	byName := map[string]bool{}
	for _, r := range records {
		byName[r.Name] = true
		if r.Purpose == "" {
			t.Errorf("record %s has no purpose; six near-identical rows need to say which one costs what", r.Name)
		}
	}

	for key, v := range greenfieldHostnames(sn) {
		host, ok := v.(string)
		if !ok {
			continue
		}
		if !byName[host] {
			t.Errorf("the deploy writes %s = %q, but no DNS record is shown for it — add %q to recordOrder", key, host, key)
		}
	}
}

// The one the wizard's footnote omitted.
func TestTheServerNameItselfNeedsARecord(t *testing.T) {
	for _, r := range greenfieldRecords("example.com") {
		if r.Name == "example.com" {
			return
		}
	}
	t.Error("well-known delegation is served at the server name itself; without that record nothing federates")
}

func TestRecordsAreNamedAfterTheServer(t *testing.T) {
	got := greenfieldRecords("example.com")
	if len(got) != len(recordOrder) {
		t.Fatalf("got %d records, want %d", len(got), len(recordOrder))
	}
	if got[0].Name != "example.com" {
		t.Errorf("first record is %q; the base domain belongs first, it is the one people forget", got[0].Name)
	}
	want := map[string]bool{
		"example.com": true, "matrix.example.com": true, "mas.example.com": true,
		"element.example.com": true, "admin.example.com": true, "mrtc.example.com": true,
	}
	for _, r := range got {
		if !want[r.Name] {
			t.Errorf("unexpected record %q", r.Name)
		}
	}
}

// The endpoint against real DNS.
//
// Everything above uses a fake resolver, which proves the code handles my idea of
// DNS. This one asks the actual resolver the pod would use, through the actual
// handler, and is guarded like the other live tests.
func TestSetupDNSAgainstRealResolver(t *testing.T) {
	if os.Getenv("RUN_LIVE") != "1" {
		t.Skip("set RUN_LIVE=1 to resolve real names")
	}
	// No cluster client: the fresh-install shape, where the target is unknown and the
	// endpoint has to say so rather than invent one.
	h := &HelmHandler{}
	rec := httptest.NewRecorder()
	h.SetupDNS(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/dns?server_name=example.com", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Target struct {
			Usable bool   `json:"usable"`
			Note   string `json:"note"`
		} `json:"target"`
		Records []struct {
			Name     string   `json:"name"`
			Status   string   `json:"status"`
			Resolved []string `json:"resolved"`
		} `json:"records"`
		AllOK bool `json:"all_ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — %s", err, rec.Body.String())
	}
	if got.Target.Usable {
		t.Error("with no cluster client there is no known target; claiming one would be inventing it")
	}
	if got.Target.Note == "" {
		t.Error("it must say why it cannot name a target")
	}
	if len(got.Records) != len(recordOrder) {
		t.Fatalf("got %d records, want %d", len(got.Records), len(recordOrder))
	}
	if got.AllOK {
		t.Error("nothing can be confirmed against an unknown target")
	}
	for _, r := range got.Records {
		t.Logf("%-24s %-10s %v", r.Name, r.Status, r.Resolved)
		if r.Status == "" {
			t.Errorf("%s came back with no status", r.Name)
		}
	}
}
