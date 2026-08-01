package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type fakeSink struct {
	records []AuditRecord
	err     error
}

func (f *fakeSink) Write(_ context.Context, rec AuditRecord) error {
	f.records = append(f.records, rec)
	return f.err
}

func serve(t *testing.T, sink AuditSink, method, path string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	}
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	Audit(sink)(handler).ServeHTTP(rr, req)
	return rr
}

func TestMutatingRequestsAreRecorded(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		sink := &fakeSink{}
		serve(t, sink, method, "/api/v1/helm/releases/ess/upgrade", nil)
		if len(sink.records) != 1 {
			t.Fatalf("%s: got %d records, want 1", method, len(sink.records))
		}
		if got := sink.records[0].Action; got != method+" /api/v1/helm/releases/ess/upgrade" {
			t.Errorf("%s: action = %q", method, got)
		}
	}
}

func TestReadsAreNotRecorded(t *testing.T) {
	// Every dashboard poll is a GET. Recording them would bury the handful of
	// entries a month that matter under thousands that do not.
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		sink := &fakeSink{}
		serve(t, sink, method, "/api/v1/status", nil)
		if len(sink.records) != 0 {
			t.Errorf("%s should not be audited, got %d records", method, len(sink.records))
		}
	}
}

func TestFailedAttemptsAreRecordedAsErrors(t *testing.T) {
	// An audit trail that lists only successes is worse than none: the rejected
	// upgrade is usually the interesting row.
	sink := &fakeSink{}
	serve(t, sink, http.MethodPost, "/api/v1/helm/releases/ess/upgrade",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })

	if len(sink.records) != 1 {
		t.Fatalf("got %d records, want 1", len(sink.records))
	}
	if sink.records[0].Result != "error" || sink.records[0].Status != http.StatusBadRequest {
		t.Errorf("got result=%q status=%d, want error/400", sink.records[0].Result, sink.records[0].Status)
	}
}

func TestHandlerThatNeverWritesHeaderCountsAsOK(t *testing.T) {
	// net/http sends 200 when a handler returns without calling WriteHeader.
	// Recording 0 here would put a status nothing else in the system uses into
	// the trail.
	sink := &fakeSink{}
	serve(t, sink, http.MethodPost, "/api/v1/hooks", func(http.ResponseWriter, *http.Request) {})

	if sink.records[0].Status != http.StatusOK || sink.records[0].Result != "ok" {
		t.Errorf("got status=%d result=%q, want 200/ok", sink.records[0].Status, sink.records[0].Result)
	}
}

func TestSinkFailureDoesNotBreakTheRequest(t *testing.T) {
	// The upgrade succeeded. A database hiccup while recording it must not turn
	// that into a failure the operator sees.
	sink := &fakeSink{err: errors.New("database gone")}
	rr := serve(t, sink, http.MethodPost, "/api/v1/helm/releases/ess/upgrade",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })

	if rr.Code != http.StatusAccepted {
		t.Errorf("response code = %d, want 202 — the audit failure leaked into the response", rr.Code)
	}
}

func TestNilSinkIsSafe(t *testing.T) {
	rr := serve(t, nil, http.MethodPost, "/api/v1/hooks", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("response code = %d with a nil sink", rr.Code)
	}
}

func TestUserIsAttributed(t *testing.T) {
	sink := &fakeSink{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hooks", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserIDKey, "@someone:example.com"))
	rr := httptest.NewRecorder()

	Audit(sink)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(rr, req)

	if got := sink.records[0].UserID; got != "@someone:example.com" {
		t.Errorf("user = %q, want the authenticated user", got)
	}
}

// The record type is the contract with the database. If someone adds a Body or
// Headers field, secrets start landing in the audit table — which is the failure
// this design exists to prevent, so it should break a test rather than a review.
func TestRecordCarriesNoPayload(t *testing.T) {
	forbidden := map[string]bool{
		"Body": true, "Headers": true, "Header": true,
		"Query": true, "Payload": true, "Request": true,
	}

	typ := reflect.TypeOf(AuditRecord{})
	for i := range typ.NumField() {
		if forbidden[typ.Field(i).Name] {
			t.Errorf("AuditRecord.%s would put request content into the audit table; "+
				"see the type's doc comment for why that is deliberate", typ.Field(i).Name)
		}
	}
}
