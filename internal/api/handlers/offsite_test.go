package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/backup/offsite"
)

type fakeBackup struct {
	configured bool
	err        error
	calls      int
	version    string
}

func (f *fakeBackup) Configured(context.Context) (bool, []string, error) {
	return f.configured, []string{"Endpunkt"}, nil
}
func (f *fakeBackup) BackupBeforeUpgrade(_ context.Context, v string, _ func(string)) (string, error) {
	f.calls++
	f.version = v
	return "p/obj", f.err
}

// The upgrade's gate decides whether helm.Upgrade is reached: the goroutine in Upgrade
// returns before it when proceed is false.
func TestUpgradeGate(t *testing.T) {
	cases := []struct {
		name        string
		b           *fakeBackup
		nilBackup   bool
		skip        bool
		wantProceed bool
		wantOutcome string
		wantCalls   int
	}{
		{"backup fails -> no upgrade", &fakeBackup{configured: true, err: errors.New("S3 weg")}, false, false, false, "failed", 1},
		{"backup fails, skip_backup -> upgrade", &fakeBackup{configured: true, err: errors.New("S3 weg")}, false, true, true, "skipped", 0},
		{"backup ok -> upgrade", &fakeBackup{configured: true}, false, false, true, "ok", 1},
		{"no target -> upgrade with note", &fakeBackup{configured: false}, false, false, true, "not_configured", 0},
		{"no service at all -> upgrade", nil, true, false, true, "not_configured", 0},
	}
	for _, c := range cases {
		h := &HelmHandler{}
		if !c.nilBackup {
			h.backup = c.b
		}
		stream := &upgradeStream{status: "pending"}
		proceed, outcome, _, err := h.backupBeforeUpgrade(context.Background(), stream, "26.5.1", c.skip)
		if proceed != c.wantProceed || outcome != c.wantOutcome {
			t.Errorf("%s: proceed=%v outcome=%s err=%v", c.name, proceed, outcome, err)
		}
		if !proceed && err == nil {
			t.Errorf("%s: blocked without a reason", c.name)
		}
		if !c.nilBackup && c.b.calls != c.wantCalls {
			t.Errorf("%s: backup calls=%d", c.name, c.b.calls)
		}
		if !c.nilBackup && c.wantCalls == 1 && c.b.version != "26.5.1" {
			t.Errorf("%s: version %q", c.name, c.b.version)
		}
	}
}

type setStore struct{ s offsite.Settings }

func (m *setStore) Load(context.Context) (offsite.Settings, error) { return m.s, nil }
func (m *setStore) Save(_ context.Context, s offsite.Settings) error {
	m.s = s
	return nil
}

type credStore struct{ c offsite.Credentials }

func (m *credStore) Get(context.Context) (offsite.Credentials, error) { return m.c, nil }
func (m *credStore) Put(_ context.Context, c offsite.Credentials) error {
	m.c = c
	return nil
}

func offsiteHandler() (*OffsiteHandler, *setStore, *credStore) {
	ss := &setStore{s: offsite.DefaultSettings()}
	cs := &credStore{}
	svc := offsite.NewService(offsite.Deps{Settings: ss, Creds: cs, Runs: nil})
	return NewOffsiteHandler(svc), ss, cs
}

func do(h http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestSettingsNeverReturnKeys(t *testing.T) {
	h, _, cs := offsiteHandler()
	rec := do(h.PutSettings, "PUT", `{"endpoint":"s3.example.org","bucket":"b","prefix":"x","schedule":true,"schedule_time":"03:30","media":true,"access_key":"AKIDSECRET","secret_key":"TOPSECRETVALUE"}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if cs.c.AccessKey != "AKIDSECRET" || cs.c.SecretKey != "TOPSECRETVALUE" {
		t.Fatal("keys not stored")
	}
	rec = do(h.GetSettings, "GET", "")
	out := rec.Body.String()
	if strings.Contains(out, "AKIDSECRET") || strings.Contains(out, "TOPSECRETVALUE") {
		t.Fatalf("key leaked: %s", out)
	}
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m["keys_set"] != true {
		t.Fatalf("keys_set: %v", m)
	}
	// Empty keys keep the stored ones.
	do(h.PutSettings, "PUT", `{"endpoint":"s3.example.org","bucket":"b","prefix":"x","schedule_time":"04:00"}`)
	if cs.c.AccessKey != "AKIDSECRET" {
		t.Fatal("empty key overwrote the stored one")
	}
}

func TestKeyPairShownOnceAndPrivateKeyNeverStored(t *testing.T) {
	h, ss, _ := offsiteHandler()
	rec := do(h.KeyPair, "POST", `{}`)
	var kp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &kp)
	if rec.Code != 200 || !strings.HasPrefix(kp["private_key"], "AGE-SECRET-KEY-") || ss.s.PublicKey != kp["public_key"] {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if b, _ := json.Marshal(ss.s); strings.Contains(string(b), kp["private_key"]) {
		t.Fatal("private key reached the stored settings")
	}
	if get := do(h.GetSettings, "GET", "").Body.String(); strings.Contains(get, kp["private_key"]) {
		t.Fatal("private key served again")
	}
	// A second pair would orphan the first's backups: refused unless asked for.
	if rec := do(h.KeyPair, "POST", `{}`); rec.Code != 409 {
		t.Fatalf("second keypair: %d", rec.Code)
	}
	// And a private key is not accepted as the public one.
	if rec := do(h.PutPublicKey, "PUT", `{"public_key":"`+kp["private_key"]+`"}`); rec.Code != 422 || strings.Contains(rec.Body.String(), kp["private_key"]) {
		t.Fatalf("private key as public: %d %s", rec.Code, rec.Body)
	}
}

func TestOpenUploadDecryptsAgeAndPassesPlain(t *testing.T) {
	pub, priv, _ := offsite.GenerateKeyPair()
	var enc bytes.Buffer
	w, _ := offsite.Encrypt(&enc, pub)
	_, _ = w.Write([]byte("hello archive"))
	_ = w.Close()

	req := httptest.NewRequest("POST", "/", bytes.NewReader(enc.Bytes()))
	req.Header.Set(ageKeyHeader, priv)
	r, err := openUpload(req)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); string(got) != "hello archive" {
		t.Fatalf("got %q", got)
	}

	// Wrong key: refused before anything is read further.
	_, other, _ := offsite.GenerateKeyPair()
	req = httptest.NewRequest("POST", "/", bytes.NewReader(enc.Bytes()))
	req.Header.Set(ageKeyHeader, other)
	if _, err := openUpload(req); err == nil {
		t.Fatal("wrong key accepted")
	}
	// Missing key.
	req = httptest.NewRequest("POST", "/", bytes.NewReader(enc.Bytes()))
	if _, err := openUpload(req); err == nil {
		t.Fatal("missing key accepted")
	}
	// A plain archive is untouched.
	req = httptest.NewRequest("POST", "/", strings.NewReader("\x1f\x8bplain"))
	r, err = openUpload(req)
	if got, _ := io.ReadAll(r); err != nil || string(got) != "\x1f\x8bplain" {
		t.Fatalf("plain changed: %q %v", got, err)
	}
}
