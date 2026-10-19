package handlers

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup/offsite"
)

// Automatic backups outside the server: the routes behind the "Automatisch" card
// (etappe 120). The work is in internal/backup/offsite; this file maps it to HTTP.
//
// Two rules for what leaves this process:
//   - the S3 access keys are never returned, only whether they are set;
//   - the private age key is returned exactly once, by KeyPair, and is neither stored
//     nor logged anywhere.

var errBackupUnavailable = errors.New("Für diese Installation ist kein Backup verfügbar.")

type OffsiteHandler struct {
	svc *offsite.Service
}

func NewOffsiteHandler(svc *offsite.Service) *OffsiteHandler { return &OffsiteHandler{svc: svc} }

// fail maps the service's errors to statuses a UI can act on.
func (h *OffsiteHandler) fail(w http.ResponseWriter, err error) {
	var nc *offsite.NotConfiguredError
	switch {
	case errors.Is(err, offsite.ErrBusy):
		Error(w, http.StatusConflict, err.Error())
	case errors.As(err, &nc):
		JSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "missing": nc.Missing})
	case errors.Is(err, offsite.ErrKeyExists):
		JSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "key_exists": true})
	default:
		Error(w, http.StatusBadGateway, err.Error())
	}
}

type offsiteSettingsBody struct {
	offsite.Settings
	// KeysSet is output only: the keys themselves are never sent back.
	KeysSet bool `json:"keys_set"`
}

// GET /api/v1/status/backup/offsite/settings
func (h *OffsiteHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	st, keys, err := h.svc.LoadSettings(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Die Einstellungen waren nicht lesbar: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, offsiteSettingsBody{Settings: st, KeysSet: keys})
}

// PUT /api/v1/status/backup/offsite/settings — empty access_key/secret_key keep the
// stored ones. public_key is ignored here; it has its own routes.
func (h *OffsiteHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		offsite.Settings
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	if err := h.svc.SaveSettings(r.Context(), offsite.SettingsInput{
		Settings: req.Settings, AccessKey: req.AccessKey, SecretKey: req.SecretKey,
	}); err != nil {
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// POST /api/v1/status/backup/offsite/probe — write, read and delete a test object in the
// saved target.
func (h *OffsiteHandler) Probe(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Probe(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	JSON(w, http.StatusOK, res)
}

// POST /api/v1/status/backup/offsite/keypair — {"replace": bool}. Answers with the
// private key once; 409 if a key exists and replace is not set.
func (h *OffsiteHandler) KeyPair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Replace bool `json:"replace"`
	}
	_ = Decode(r, &req)
	pub, priv, err := h.svc.NewKeyPair(r.Context(), req.Replace)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	JSON(w, http.StatusOK, map[string]string{"public_key": pub, "private_key": priv})
}

// PUT /api/v1/status/backup/offsite/public-key — {"public_key": "age1…"}: the operator's
// own key. A private key in this field is refused.
func (h *OffsiteHandler) PutPublicKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicKey string `json:"public_key"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	if err := h.svc.SetPublicKey(r.Context(), req.PublicKey); err != nil {
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// POST /api/v1/status/backup/offsite/run — "Jetzt sichern". Starts in the background (it
// outlasts any proxy) and answers 202; progress is GET .../status.
func (h *OffsiteHandler) Run(w http.ResponseWriter, r *http.Request) {
	st, _, err := h.svc.LoadSettings(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	media := st.Media
	var req struct {
		Media *bool `json:"media"`
	}
	if Decode(r, &req) == nil && req.Media != nil {
		media = *req.Media
	}
	if err := h.svc.StartBackup(offsite.BackupRequest{Occasion: offsite.OccasionManual, Media: media}); err != nil {
		h.fail(w, err)
		return
	}
	JSON(w, http.StatusAccepted, map[string]any{"started": true, "media": media})
}

// GET /api/v1/status/backup/offsite/objects — the backups in the bucket, newest first.
func (h *OffsiteHandler) List(w http.ResponseWriter, r *http.Request) {
	entries, err := h.svc.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	if entries == nil {
		entries = []offsite.Entry{}
	}
	JSON(w, http.StatusOK, map[string]any{"objects": entries})
}

// GET /api/v1/status/backup/offsite/objects/download?name=… — the encrypted file as a
// stream. It is not decrypted here: opening it takes the operator's private key, which
// the download must not need.
func (h *OffsiteHandler) Download(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	rc, size, err := h.svc.OpenObject(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("offsite download: failed partway through: %v", err)
	}
}

// GET /api/v1/status/backup/offsite/status — setup state, the last runs, the next one.
func (h *OffsiteHandler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Status(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, st)
}

// UpgradeBackup adapts the offsite service to the upgrade's needs.
type UpgradeBackup struct{ Svc *offsite.Service }

func (u UpgradeBackup) Configured(ctx context.Context) (bool, []string, error) {
	return u.Svc.Configured(ctx)
}

// BackupBeforeUpgrade leaves the media out: an upgrade does not touch them, and a
// gigabytes-long upload in front of every upgrade would be a reason to skip the backup.
func (u UpgradeBackup) BackupBeforeUpgrade(ctx context.Context, version string, say func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	res, err := u.Svc.Backup(ctx, offsite.BackupRequest{
		Occasion: offsite.OccasionUpgrade(version), Media: false, Progress: say,
	})
	return res.Key, err
}
