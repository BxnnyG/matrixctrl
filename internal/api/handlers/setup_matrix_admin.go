package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/auth"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/mas"
	"gopkg.in/yaml.v3"
)

// masAdmin builds an admin client for MAS out of the credentials MatrixCtrl registered
// there, without going through its own login configuration.
//
// The order this exists for: registering the client, creating the first account and
// switching MatrixCtrl's sign-in over are three separate steps, and the middle one
// needs an admin token. AuthHandler.MAS() only answers after the third — so before
// this, a freshly deployed homeserver could not be given its first account by the
// product that had just deployed it (§4.88).
func (h *HelmHandler) masAdmin(ctx context.Context) (*mas.Client, string, error) {
	client, id, _, _, err := h.registeredMASClient(ctx)
	return client, id, err
}

// registeredMASClient is masAdmin plus the credentials themselves, for the one caller
// that has to finish a switch-over the first attempt left half-done.
func (h *HelmHandler) registeredMASClient(ctx context.Context) (*mas.Client, string, string, string, error) {
	contents, err := h.configStore.MergedContent(ctx)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("read configuration: %w", err)
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("read configuration: %w", err)
	}

	fragment, _ := nestedGet(merged, "matrixAuthenticationService", "additional", "0-matrixctrl-client", "config").(string)
	if strings.TrimSpace(fragment) == "" {
		return nil, "", "", "", fmt.Errorf("MatrixCtrl ist bei MAS noch nicht registriert")
	}
	var clients []struct {
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
	}
	if err := yaml.Unmarshal([]byte(fragment), &clients); err != nil || len(clients) == 0 {
		return nil, "", "", "", fmt.Errorf("die registrierte MAS-Client-Konfiguration ist nicht lesbar")
	}
	id, secret := clients[0].ClientID, clients[0].ClientSecret
	if id == "" || secret == "" {
		return nil, "", "", "", fmt.Errorf("die registrierte MAS-Client-Konfiguration ist unvollständig")
	}

	host, _ := nestedGet(merged, "matrixAuthenticationService", "ingress", "host").(string)
	if host == "" {
		return nil, "", "", "", fmt.Errorf("in der Konfiguration steht keine MAS-Adresse")
	}

	issuer := "https://" + host
	client, err := auth.MASAdminClient(ctx, issuer, id, secret)
	if err != nil {
		return nil, id, secret, issuer, err
	}
	return client, id, secret, issuer, nil
}

type matrixAdminsResponse struct {
	// Available is false when MAS cannot be asked at all — not registered yet, or not
	// answering. Reason says which, because those are opposite problems.
	Available bool     `json:"available"`
	Reason    string   `json:"reason,omitempty"`
	Admins    []string `json:"admins"`
	// ClientKnown is MAS's own answer to "do you know MatrixCtrl", as opposed to the
	// configuration file's answer, which is what the product used to trust.
	ClientKnown bool `json:"client_known"`
}

// GET /api/v1/setup/matrix-admins — is there anybody to log in as?
func (h *HelmHandler) MatrixAdmins(w http.ResponseWriter, r *http.Request) {
	out := matrixAdminsResponse{Admins: []string{}}

	client, clientID, err := h.masAdmin(r.Context())
	if err != nil {
		out.Reason = err.Error()
		JSON(w, http.StatusOK, out)
		return
	}

	if known, err := client.ClientKnown(r.Context(), clientID); err == nil {
		out.ClientKnown = known
	}

	page, err := client.ListUsers(r.Context(), mas.UserQuery{AdminOnly: true, Limit: 25})
	if err != nil {
		out.Reason = "MAS antwortet nicht: " + err.Error()
		JSON(w, http.StatusOK, out)
		return
	}
	out.Available = true
	for _, u := range page.Users {
		out.Admins = append(out.Admins, u.Username)
	}
	JSON(w, http.StatusOK, out)
}

// POST /api/v1/setup/matrix-admin — create the first account and make it an admin.
//
// Three calls, in an order that cannot leave a half-made account behind in a way that
// matters: create, set the password, grant admin. If the last one fails the account
// exists and can be promoted by hand or by pressing this again; if the first fails
// nothing happened at all.
func (h *HelmHandler) CreateMatrixAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		Error(w, http.StatusBadRequest, "Benutzername und Passwort werden beide gebraucht.")
		return
	}

	client, _, err := h.masAdmin(r.Context())
	if err != nil {
		Error(w, http.StatusServiceUnavailable, "MAS ist noch nicht erreichbar: "+err.Error())
		return
	}

	user, err := client.CreateUser(r.Context(), req.Username, "")
	if err != nil {
		writeMASError(w, err)
		return
	}
	if user == nil {
		Error(w, http.StatusBadGateway, "MAS hat das Konto angelegt, aber nichts darüber zurückgemeldet.")
		return
	}
	if err := client.SetPassword(r.Context(), user.ID, req.Password); err != nil {
		writeMASError(w, err)
		return
	}
	if err := client.SetAdmin(r.Context(), user.ID, true); err != nil {
		writeMASError(w, err)
		return
	}

	// The password is never echoed back, never logged and never put in a path — the
	// same rule SetPassword documents.
	_ = authmw.UserIDFromContext(r.Context())
	JSON(w, http.StatusOK, map[string]any{
		"username": user.Username,
		"id":       user.ID,
		"admin":    true,
	})
}

// writeMASError turns MAS's own refusals into the status they deserve.
func writeMASError(w http.ResponseWriter, err error) {
	var ae *mas.ActionError
	if errors.As(err, &ae) {
		Error(w, ae.Status, ae.Msg)
		return
	}
	Error(w, http.StatusBadGateway, err.Error())
}
