package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/backup"
)

// packArchive builds a real .tar.gz in the layout Read expects, so the test exercises
// the parser rather than a stand-in for it.
func packArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The archive knows which server it came from, and the preview now says so.
//
// That one field is the difference between "restore this onto a server you prepared
// yourself" and "this archive can rebuild the server". The operator asked why the ESS
// version could not be read from the backup and deployed; the server name is the other
// half of the same answer (etappe 82).
func TestPreviewReportsTheServerNameFromTheArchive(t *testing.T) {
	manifest, err := json.Marshal(backup.Manifest{
		FormatVersion: backup.FormatVersion,
		AppVersion:    "0.1.70",
		ESS:           backup.Release{Name: "ess", Chart: "matrix-stack-26.8.0", Revision: 30},
		ConfigFiles:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := packArchive(t, map[string]string{
		"manifest.json":            string(manifest),
		"config-repo/general.yaml": "serverName: example.com\n",
		"config-repo/synapse.yaml": "synapse:\n  ingress:\n    host: matrix.example.com\n",
		// Must not be parsed as YAML — the repository carries its own git objects.
		"config-repo/.git/HEAD": "ref: refs/heads/master\n",
	})

	h := &StatusHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/status/restore/preview", bytes.NewReader(body))
	h.RestorePreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		ServerName string `json:"server_name"`
		AppVersion string `json:"app_version"`
		ESS        struct {
			Chart    string `json:"chart"`
			Revision int    `json:"revision"`
		} `json:"ess"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — %s", err, rec.Body.String())
	}
	if got.ServerName != "example.com" {
		t.Errorf("server_name = %q, want example.com — without it the migration path has to ask for something the archive already knows", got.ServerName)
	}
	// The manifest's own fields must survive being embedded alongside the derived one.
	if got.AppVersion != "0.1.70" || got.ESS.Chart != "matrix-stack-26.8.0" || got.ESS.Revision != 30 {
		t.Errorf("manifest fields lost: %+v", got)
	}
}

// An archive without a server name is still restorable; the operator names the server.
// Guessing one would be worse than admitting there is none.
func TestPreviewWithoutAServerNameSaysNothing(t *testing.T) {
	manifest, _ := json.Marshal(backup.Manifest{FormatVersion: backup.FormatVersion})
	body := packArchive(t, map[string]string{
		"manifest.json":            string(manifest),
		"config-repo/synapse.yaml": "synapse:\n  enabled: true\n",
	})

	rec := httptest.NewRecorder()
	(&StatusHandler{}).RestorePreview(rec, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)))

	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if v, present := got["server_name"]; present {
		t.Errorf("server_name = %v; it must be absent rather than empty or invented", v)
	}
}
