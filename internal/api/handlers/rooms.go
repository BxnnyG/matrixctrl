package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/synapse"
)

// Rooms live in Synapse, not MAS, and are read with the *operator's* authority — the
// `urn:synapse:admin:*` scope, granted by MAS only to accounts with
// `can_request_admin`. The panel can therefore do what the person signed into it can
// do, and only while they are signed in (etappe 36).

// RoomsHandler serves the room list.
type RoomsHandler struct {
	// client is built per request because the token belongs to the caller, not to the
	// process. A client captured at construction would carry one operator's authority
	// into another operator's request.
	client func(userID string) *synapse.Client
	// connected reports whether this process holds a Matrix session for the user.
	connected func(userID string) bool
	// authURL starts the separate authorization that grants the scope.
	authURL func(ctx context.Context, returnTo string) (string, error)
}

func NewRoomsHandler(
	client func(userID string) *synapse.Client,
	connected func(userID string) bool,
	authURL func(ctx context.Context, returnTo string) (string, error),
) *RoomsHandler {
	return &RoomsHandler{client: client, connected: connected, authURL: authURL}
}

// roomsState is what the page needs before it can decide what to render.
type roomsState struct {
	// Connected is false after every restart, because the refresh token is held in
	// memory only and deliberately not persisted. That is an ordinary state with a
	// one-click fix, not an error.
	Connected bool   `json:"connected"`
	Reason    string `json:"reason,omitempty"`
}

// GET /api/v1/rooms/state — can this operator read rooms, and if not, why not.
//
// Separate from the list itself so the page can render the right thing immediately
// instead of showing a table that then fails.
func (h *RoomsHandler) State(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	if h.connected == nil || !h.connected(userID) {
		JSON(w, http.StatusOK, roomsState{
			Connected: false,
			Reason:    "Für Räume wird einmalig der Matrix-Admin-Zugriff verbunden.",
		})
		return
	}
	JSON(w, http.StatusOK, roomsState{Connected: true})
}

// POST /api/v1/rooms/connect — begins the authorization that grants the scope.
//
// POST rather than GET: it mints a state row and starts an authorization, and the
// audit middleware records mutations.
func (h *RoomsHandler) Connect(w http.ResponseWriter, r *http.Request) {
	if h.authURL == nil {
		Error(w, http.StatusNotImplemented, "Matrix-Login ist nicht konfiguriert")
		return
	}
	// Which screen asked. Sent by the caller and never trusted: the callback maps it
	// onto an allowlist before redirecting anywhere (etappe 52).
	var body struct {
		ReturnTo string `json:"return_to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	url, err := h.authURL(r.Context(), body.ReturnTo)
	if err != nil {
		Error(w, http.StatusBadGateway, "Die Anmeldung bei Matrix konnte nicht gestartet werden")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"url": url})
}

// GET /api/v1/rooms — one page of rooms.
func (h *RoomsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	client := h.client(userID)
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}

	q := r.URL.Query()
	opts := synapse.ListOptions{
		SearchTerm: q.Get("search"),
		OrderBy:    q.Get("order_by"),
		Dir:        q.Get("dir"),
	}
	// Ignoring the error is deliberate: a non-numeric page is a broken link, not a
	// reason to refuse. Zero is the first page, which is what a reader wanted.
	opts.From, _ = strconv.Atoi(q.Get("from"))
	opts.Limit, _ = strconv.Atoi(q.Get("limit"))

	page, err := client.ListRooms(r.Context(), opts)
	if err != nil {
		writeSynapseError(w, err, "Die Raumliste konnte nicht geladen werden.")
		return
	}
	JSON(w, http.StatusOK, page)
}

// writeSynapseError keeps apart the two failures the operator must act on differently.
//
// 401 means this process has no usable token — sign in again, and the page offers the
// button. 403 means the account is not a Synapse admin, which signing in again will
// never fix. Collapsing them into one message sends the operator round a loop with no
// exit.
//
// `subject` names what could not be loaded and is used for the fallback only. The two
// branches above it concern the *token* and read identically whatever was being
// fetched, which is why they are shared rather than re-derived by every screen that
// talks to Synapse (etappe 46).
func writeSynapseError(w http.ResponseWriter, err error, subject string) {
	if subject == "" {
		subject = "Die Daten konnten nicht geladen werden."
	}
	var se *synapse.Error
	if errors.As(err, &se) {
		switch {
		case se.NeedsLogin():
			// 409, not 401. In this API 401 means the *MatrixCtrl* session is invalid,
			// and the frontend ends the session on sight — correct for that meaning and
			// disastrous here, where the only thing that expired is a downstream Matrix
			// token. Answering 401 would sign the operator out of the panel every time
			// their five-minute Synapse token lapsed.
			Error(w, http.StatusConflict,
				"Der Matrix-Zugriff ist abgelaufen — bitte erneut verbinden.")
			return
		case se.NotAdmin():
			Error(w, http.StatusForbidden,
				"Dieses Konto hat keine Synapse-Administratorrechte.")
			return
		}
	}
	Error(w, http.StatusBadGateway, subject)
}

// GET /api/v1/rooms/{id} — one room, with its block state.
func (h *RoomsHandler) Detail(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	room, err := client.GetRoom(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeSynapseError(w, err, "Der Raum konnte nicht geladen werden.")
		return
	}
	JSON(w, http.StatusOK, room)
}

// GET /api/v1/rooms/{id}/members — one page of joined members.
func (h *RoomsHandler) Members(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	q := r.URL.Query()
	from, _ := strconv.Atoi(q.Get("from"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	page, err := client.ListMembers(r.Context(), chi.URLParam(r, "id"), from, limit)
	if err != nil {
		writeSynapseError(w, err, "Die Mitgliederliste konnte nicht geladen werden.")
		return
	}
	JSON(w, http.StatusOK, page)
}

// PUT /api/v1/rooms/{id}/block — refuse or allow new joins.
//
// The only room moderation this etappe ships, because it is the only one that can be
// undone. Deleting a room evicts every member and purges its history; that gets its
// own etappe, as user deactivation did.
func (h *RoomsHandler) Block(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}

	var body struct {
		Block *bool `json:"block"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Block == nil {
		// A missing field is refused rather than defaulted. Guessing wrong here
		// either blocks a room nobody asked to block or silently declines to.
		Error(w, http.StatusBadRequest, "Es fehlt die Angabe, ob der Raum gesperrt werden soll.")
		return
	}

	blocked, err := client.SetBlocked(r.Context(), chi.URLParam(r, "id"), *body.Block)
	if err != nil {
		writeSynapseError(w, err, "Die Sperre konnte nicht geändert werden.")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"blocked": blocked})
}
