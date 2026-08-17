package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/synapse"
)

// The event report queue (etappe 46).
//
// Reads Synapse with the operator's authority, exactly as rooms do — the same
// client, the same 409-not-401 convention for an expired Matrix token, the same
// connect panel on the frontend. What is new here is *disposition*: whether an admin
// has dealt with a report. Synapse has no such concept, and its only way to clear the
// queue destroys the record, so the decision is stored here instead (migration 013).

// ReportsHandler serves the queue.
type ReportsHandler struct {
	client       func(userID string) *synapse.Client
	dispositions *synapse.Dispositions
}

func NewReportsHandler(client func(userID string) *synapse.Client, d *synapse.Dispositions) *ReportsHandler {
	return &ReportsHandler{client: client, dispositions: d}
}

// reportRow is a report with what this panel knows about it.
type reportRow struct {
	synapse.Report
	// State is always populated — "open" when no decision exists — so the frontend
	// never has to treat an absent field as a meaning.
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
	Actor string `json:"actor,omitempty"`
}

// GET /api/v1/reports — one page of the queue, newest first.
func (h *ReportsHandler) List(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}

	q := r.URL.Query()
	opts := synapse.ReportOptions{Dir: q.Get("dir"), UserID: q.Get("user_id"), RoomID: q.Get("room_id")}
	opts.From, _ = strconv.Atoi(q.Get("from"))
	opts.Limit, _ = strconv.Atoi(q.Get("limit"))

	page, err := client.ListReports(r.Context(), opts)
	if err != nil {
		writeSynapseError(w, err, "Die Meldungen konnten nicht geladen werden.")
		return
	}

	ids := make([]int64, 0, len(page.Reports))
	for _, rep := range page.Reports {
		ids = append(ids, rep.ID)
	}
	// One query for the page. A failure here costs the badges, not the queue: the
	// reports are the thing being asked for, and refusing to show them because their
	// annotations could not be read would be the wrong trade.
	decided, _ := h.dispositions.For(r.Context(), ids)

	rows := make([]reportRow, 0, len(page.Reports))
	open := 0
	for _, rep := range page.Reports {
		row := reportRow{Report: rep, State: synapse.StateOpen}
		if d, ok := decided[rep.ID]; ok {
			row.State, row.Note, row.Actor = d.State, d.Note, d.Actor
		} else {
			open++
		}
		rows = append(rows, row)
	}

	JSON(w, http.StatusOK, map[string]any{
		"reports": rows,
		"total":   page.Total,
		// open counts this page only, and is named for what it is. A queue-wide count
		// would mean walking every page on every load to annotate a badge.
		"open_on_page": open,
		"next_token":   page.NextToken,
	})
}

// GET /api/v1/reports/{id} — one report with the reported event.
func (h *ReportsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Melde-ID.")
		return
	}

	detail, err := client.GetReport(r.Context(), id)
	if err != nil {
		writeSynapseError(w, err, "Die Meldung konnte nicht geladen werden.")
		return
	}

	row := reportRow{Report: detail.Report, State: synapse.StateOpen}
	if decided, err := h.dispositions.For(r.Context(), []int64{id}); err == nil {
		if d, ok := decided[id]; ok {
			row.State, row.Note, row.Actor = d.State, d.Note, d.Actor
		}
	}

	// Media referenced by the reported event, each with the quarantine state read
	// from Synapse. Read rather than assumed: the quarantine endpoint answers `200
	// {}` whatever it did, so the only reliable source is a GET per item (E47).
	type mediaRow struct {
		synapse.MediaRef
		MXC         string `json:"mxc"`
		Quarantined bool   `json:"quarantined"`
		By          string `json:"by,omitempty"`
		Protected   bool   `json:"protected"`
		// Unknown marks an item whose state could not be read — a remote item this
		// server has never cached, most often. Distinct from "not quarantined".
		Unknown bool `json:"unknown,omitempty"`
	}
	refs := synapse.MediaInEvent(detail.EventJSON)
	media := make([]mediaRow, 0, len(refs))
	for _, ref := range refs {
		row := mediaRow{MediaRef: ref, MXC: ref.MXC()}
		if info, err := client.GetMedia(r.Context(), ref.Server, ref.ID); err == nil {
			row.Quarantined, row.By, row.Protected = info.Quarantined(), info.QuarantinedBy, info.SafeFromQuarantine
		} else {
			row.Unknown = true
		}
		media = append(media, row)
	}

	JSON(w, http.StatusOK, map[string]any{
		"report": row,
		// The reported event's readable parts, extracted server-side so the shape of a
		// Matrix event is not a thing the frontend has to know. Empty body with a type
		// of m.room.encrypted is a meaningful answer, not a failure.
		"body":       synapse.ReportedBody(detail.EventJSON),
		"event_type": synapse.ReportedEventType(detail.EventJSON),
		"media":      media,
		"event_json": detail.EventJSON,
	})
}

// PUT /api/v1/media/{server}/{id}/quarantine — quarantine or release one item.
//
// The response reports what Synapse holds *afterwards*, not what was requested. Its
// own endpoint returns `200 {}` in every case and silently skips protected media, so
// echoing the request back would be a confident lie in the one case that matters.
func (h *ReportsHandler) Quarantine(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}

	var body struct {
		Quarantine *bool `json:"quarantine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Quarantine == nil {
		Error(w, http.StatusBadRequest, "Es fehlt die Angabe, ob die Datei gesperrt werden soll.")
		return
	}

	res, err := client.SetQuarantined(r.Context(),
		chi.URLParam(r, "server"), chi.URLParam(r, "id"), *body.Quarantine)
	if err != nil {
		writeSynapseError(w, err, "Die Sperre konnte nicht geändert werden.")
		return
	}
	JSON(w, http.StatusOK, res)
}

// PUT /api/v1/reports/{id}/disposition — mark handled or dismissed, or reopen.
//
// Nothing is sent to Synapse. Its record of the report is left exactly as it is —
// see migration 013 for why deleting it would destroy the pattern that matters.
func (h *ReportsHandler) SetDisposition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Melde-ID.")
		return
	}

	var body struct {
		State string `json:"state"`
		Note  string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "Ungültige Anfrage.")
		return
	}

	actor := authmw.UserIDFromContext(r.Context())

	if body.State == synapse.StateOpen {
		if err := h.dispositions.Reopen(r.Context(), id); err != nil {
			Error(w, http.StatusInternalServerError, "Die Meldung konnte nicht erneut geöffnet werden.")
			return
		}
		JSON(w, http.StatusOK, map[string]any{"state": synapse.StateOpen})
		return
	}

	if !synapse.ValidState(body.State) {
		Error(w, http.StatusBadRequest, "Unbekannter Status.")
		return
	}
	if err := h.dispositions.Set(r.Context(), id, body.State, body.Note, actor); err != nil {
		Error(w, http.StatusInternalServerError, "Der Status konnte nicht gespeichert werden.")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"state": body.State, "note": body.Note, "actor": actor})
}
