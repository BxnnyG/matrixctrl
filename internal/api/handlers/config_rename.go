package handlers

import (
	"context"
	"net/http"
	"strings"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/config"
)

// Renaming the server touches every section that carries a hostname.
//
// Done by hand it is six edits in five files, and the one that is always forgotten is
// `serverName` itself — well-known delegation is served there, so a rename that misses
// it leaves federation pointing at a domain nobody answers on (§4.81).
//
// It is not hypothetical here: restoring a backup onto a different server brings the
// old domain with it, in every hostname the archive carried (etappe 82).

type renameChange struct {
	// Key is the config path, the same one the greenfield deploy writes.
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
	// Derived is false when the current value is not what the old server name would
	// have produced — a host somebody chose by hand. Those are shown and left alone
	// unless the operator asks for them by name: a rename must not quietly undo a
	// decision it did not make.
	Derived bool   `json:"derived"`
	Purpose string `json:"purpose,omitempty"`
}

type renamePreview struct {
	CurrentServerName string         `json:"current_server_name"`
	NewServerName     string         `json:"new_server_name"`
	Changes           []renameChange `json:"changes"`
	// Unchanged lists keys whose value already matches the target — after a partial
	// rename, saying "nothing to do here" is more useful than omitting the row.
	Unchanged []string `json:"unchanged"`
}

// plan works out what a rename would touch, without touching anything.
func (h *ConfigHandler) renamePlan(ctx context.Context, newName string) (*renamePreview, error) {
	contents, err := h.store.MergedContent(ctx)
	if err != nil {
		return nil, err
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		return nil, err
	}

	return renamePlanFrom(merged, newName), nil
}

// renamePlanFrom is the whole decision, separated from where the configuration came
// from — so what it decides can be tested without a git repository behind it.
func renamePlanFrom(merged map[string]interface{}, newName string) *renamePreview {
	current, _ := merged["serverName"].(string)
	out := &renamePreview{
		CurrentServerName: current,
		NewServerName:     newName,
		Changes:           []renameChange{},
		Unchanged:         []string{},
	}

	// The same derivation the deploy uses, for both the old and the new name — so
	// "was this value derived?" is answered by comparing against what the old name
	// would have produced, not by guessing at prefixes.
	wasDerived := greenfieldHostnames(current)
	willBe := greenfieldHostnames(newName)

	for _, key := range recordOrder {
		want, _ := willBe[key].(string)
		if want == "" {
			continue
		}
		have := nestedString(merged, key)
		if have == want {
			out.Unchanged = append(out.Unchanged, key)
			continue
		}
		old, _ := wasDerived[key].(string)
		out.Changes = append(out.Changes, renameChange{
			Key:     key,
			From:    have,
			To:      want,
			Derived: current != "" && have == old,
			Purpose: recordPurpose[key],
		})
	}
	return out
}

// nestedString reads a dotted config path.
func nestedString(m map[string]interface{}, key string) string {
	parts := strings.Split(key, ".")
	cur := interface{}(m)
	for _, p := range parts {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = asMap[p]
	}
	s, _ := cur.(string)
	return s
}

// GET /api/v1/config/rename/preview?server_name=example.com
func (h *ConfigHandler) RenamePreview(w http.ResponseWriter, r *http.Request) {
	newName := strings.TrimSpace(r.URL.Query().Get("server_name"))
	if newName == "" || !strings.Contains(newName, ".") {
		Error(w, http.StatusBadRequest, "Bitte einen Domainnamen angeben, z. B. example.com")
		return
	}
	plan, err := h.renamePlan(r.Context(), newName)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Die Konfiguration ist nicht lesbar: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, plan)
}

// POST /api/v1/config/rename — apply the rename as one commit.
//
// Only the keys the caller names are written. The preview says which ones look derived
// and which do not, and the decision about a hand-picked hostname stays with the person
// who picked it.
func (h *ConfigHandler) Rename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerName string   `json:"server_name"`
		Keys       []string `json:"keys"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}
	req.ServerName = strings.TrimSpace(req.ServerName)
	if req.ServerName == "" || !strings.Contains(req.ServerName, ".") {
		Error(w, http.StatusBadRequest, "Bitte einen Domainnamen angeben, z. B. example.com")
		return
	}
	if len(req.Keys) == 0 {
		Error(w, http.StatusBadRequest, "Es wurde nichts zum Ändern ausgewählt.")
		return
	}

	plan, err := h.renamePlan(r.Context(), req.ServerName)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Die Konfiguration ist nicht lesbar: "+err.Error())
		return
	}
	// The plan is recomputed here rather than trusted from the client: what gets
	// written is derived from the configuration as it is now, not from what a browser
	// saw some minutes ago.
	wanted := map[string]bool{}
	for _, k := range req.Keys {
		wanted[k] = true
	}
	changes := map[string]interface{}{}
	applied := make([]string, 0, len(req.Keys))
	for _, c := range plan.Changes {
		if wanted[c.Key] {
			changes[c.Key] = c.To
			applied = append(applied, c.Key)
		}
	}
	if len(changes) == 0 {
		Error(w, http.StatusConflict, "Nichts davon ist noch zu ändern — die Konfiguration hat sich inzwischen geändert.")
		return
	}

	if err := h.store.SetSectionValues(r.Context(), changes, nil); err != nil {
		Error(w, http.StatusInternalServerError, "Schreiben fehlgeschlagen: "+err.Error())
		return
	}
	// One commit, because it is one decision. Renaming half a server is not a state
	// anybody wants to roll back to.
	userID := authmw.UserIDFromContext(r.Context())
	if _, err := h.store.Commit(r.Context(), "config: Server umbenannt auf "+req.ServerName, userID); err != nil {
		// Written but not committed: the values are live, the history is not. Said
		// plainly rather than reported as failure — the same line the OIDC path draws.
		JSON(w, http.StatusOK, map[string]any{
			"applied": applied,
			"warning": "Die Änderungen stehen in der Konfiguration, konnten aber nicht committet werden: " + err.Error(),
		})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"applied": applied})
}
