package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/config"
	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
)

const smtpPassword = "hunter2-smtp-not-in-git"

func mailFixture(t *testing.T) (*MailHandler, *fakeCluster, string) {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "config-slices.json"),
		[]byte(`{"slices":[{"name":"matrixAuthenticationService","file":"matrixAuthenticationService.yaml"},{"name":"general","file":"general.yaml"}]}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "matrixAuthenticationService.yaml"), []byte(masSection), 0o644))
	must(os.WriteFile(filepath.Join(dir, "general.yaml"), []byte("serverName: example.org\n"), 0o644))
	repo, err := gitpkg.OpenOrInit(dir)
	must(err)
	_, err = repo.CommitAll("init", "t", "t@example.org")
	must(err)
	fc := &fakeCluster{secrets: map[string]map[string][]byte{}}
	return NewMailHandler(fc, config.NewStore(dir, repo), "ess", "ess"), fc, dir
}

// The SMTP password goes into the Secret and nowhere else — not into a response, not
// into the settings repository.
func TestTheSMTPPasswordStaysInTheSecret(t *testing.T) {
	h, fc, dir := mailFixture(t)
	body := `{"enabled":true,"from":"noreply@example.org","host":"mail.example.org","port":587,"encryption":"starttls","username":"u","password":"` + smtpPassword + `"}`
	rec := call(t, h.Put, "PUT", "/", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"next":"apply"`) {
		t.Errorf("first save should add the mount as a pending change: %s", rec.Body)
	}
	get := call(t, h.Get, "GET", "/", "", nil)
	for i, r := range []string{rec.Body.String(), get.Body.String()} {
		if strings.Contains(r, smtpPassword) {
			t.Errorf("response %d carries the password: %s", i, r)
		}
	}
	if !strings.Contains(get.Body.String(), `"has_password":true`) {
		t.Errorf("the page must know a password is stored: %s", get.Body)
	}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), smtpPassword) {
			t.Errorf("the settings repository holds the password in %s", strings.TrimPrefix(p, dir))
		}
		return nil
	})
	if !strings.Contains(string(fc.secrets["ess/"+mailSecret][mailConfigKey]), smtpPassword) {
		t.Error("the Secret MAS reads should hold it")
	}
	mas, _ := os.ReadFile(filepath.Join(dir, "matrixAuthenticationService.yaml"))
	if !strings.Contains(string(mas), "configSecret: "+mailSecret) {
		t.Errorf("mount missing:\n%s", mas)
	}
}

// Saving again without a password keeps the stored one — the browser never had it.
func TestAnEmptyPasswordKeepsTheStoredOne(t *testing.T) {
	h, fc, _ := mailFixture(t)
	call(t, h.Put, "PUT", "/", `{"enabled":true,"from":"a@example.org","host":"m.example.org","port":587,"encryption":"starttls","username":"u","password":"`+smtpPassword+`"}`, nil)
	call(t, h.Put, "PUT", "/", `{"enabled":true,"from":"b@example.org","host":"m.example.org","port":587,"encryption":"starttls","username":"u"}`, nil)
	doc := string(fc.secrets["ess/"+mailSecret][mailConfigKey])
	if !strings.Contains(doc, smtpPassword) || !strings.Contains(doc, "b@example.org") {
		t.Errorf("password lost or sender not updated:\n%s", doc)
	}
}

// Switched off: blackhole, credentials gone from what MAS reads, sender kept.
func TestSwitchingOffKeepsTheSenderButNotTheCredentials(t *testing.T) {
	h, fc, _ := mailFixture(t)
	call(t, h.Put, "PUT", "/", `{"enabled":true,"from":"a@example.org","host":"m.example.org","port":587,"encryption":"starttls","username":"u","password":"`+smtpPassword+`"}`, nil)
	call(t, h.Put, "PUT", "/", `{"enabled":false,"from":"a@example.org","host":"m.example.org","port":587,"encryption":"starttls","username":"u"}`, nil)
	doc := string(fc.secrets["ess/"+mailSecret][mailConfigKey])
	if !strings.Contains(doc, "transport: blackhole") || strings.Contains(doc, smtpPassword) {
		t.Errorf("off:\n%s", doc)
	}
	// Still known for switching back on: the assistant reads it, the page does not.
	get := call(t, h.Get, "GET", "/", "", nil)
	var out struct {
		Settings struct {
			Host        string `json:"host"`
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(get.Body.Bytes(), &out)
	if out.Settings.Host != "m.example.org" || out.Settings.Username != "u" || !out.Settings.HasPassword {
		t.Errorf("switching off must not lose the server: %+v", out.Settings)
	}
	if strings.Contains(get.Body.String(), smtpPassword) {
		t.Error("and still never returns the password")
	}
}

func TestAnInvalidSenderIsRefused(t *testing.T) {
	h, _, _ := mailFixture(t)
	rec := call(t, h.Put, "PUT", "/", `{"enabled":true,"from":"kein-absender","host":"m.example.org","port":587}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

// The suggestion uses the server's own name, so the required field is not empty.
func TestTheSuggestedSenderUsesTheServerName(t *testing.T) {
	h, _, _ := mailFixture(t)
	if !strings.Contains(call(t, h.Get, "GET", "/", "", nil).Body.String(), `"suggested_from":"noreply@example.org"`) {
		t.Error("suggestion missing")
	}
}

var _ = context.Background
var _ = httptest.NewRequest
