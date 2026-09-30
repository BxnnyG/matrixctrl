package handlers

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/federation"
	"github.com/bxnnyg/matrixctrl/internal/synapse"
)

// FederationHandler answers the two questions an operator has about federation: can
// other servers reach mine, and which of the servers mine talks to are failing
// (etappe 117).
type FederationHandler struct {
	store *config.Store
	check func(ctx context.Context, serverName string) federation.Report
	// client is per request, for the same reason as on rooms: the Synapse admin
	// authority belongs to the operator asking, not to the process.
	client func(userID string) *synapse.Client
}

func NewFederationHandler(store *config.Store, check func(context.Context, string) federation.Report, client func(string) *synapse.Client) *FederationHandler {
	return &FederationHandler{store: store, check: check, client: client}
}

func (h *FederationHandler) serverName(ctx context.Context) string {
	if h.store == nil {
		return ""
	}
	contents, err := h.store.MergedContent(ctx)
	if err != nil {
		return ""
	}
	m, err := config.MergeToMap(contents)
	if err != nil {
		return ""
	}
	sn, _ := m["serverName"].(string)
	return strings.TrimSpace(sn)
}

// GET /api/v1/federation/reach — the path a remote server walks to reach this one.
func (h *FederationHandler) Reach(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	sn := h.serverName(ctx)
	if sn == "" {
		JSON(w, http.StatusOK, map[string]any{"note": "In den Einstellungen ist noch kein Server-Name eingetragen."})
		return
	}
	JSON(w, http.StatusOK, h.check(ctx, sn))
}

// destinationView is a destination with the arithmetic done: the UI should not need
// to know that the next retry is last attempt plus interval.
type destinationView struct {
	synapse.Destination
	Failing bool `json:"failing"`
	// NextRetryTS is when Synapse tries again on its own, ms since the epoch; 0 when
	// nothing is pending.
	NextRetryTS int64 `json:"next_retry_ts,omitempty"`
}

// GET /api/v1/federation/destinations — every server this one talks to, failing first.
func (h *FederationHandler) Destinations(w http.ResponseWriter, r *http.Request) {
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	all, cut, err := client.AllDestinations(r.Context())
	if err != nil {
		writeSynapseError(w, err, "Die Gegenstellen konnten nicht geladen werden.")
		return
	}
	views, failing := destinationViews(all)
	JSON(w, http.StatusOK, map[string]any{
		"destinations": views,
		"total":        len(views),
		"failing":      failing,
		"cut":          cut,
	})
}

// destinationViews orders failing destinations first — longest failing first, since
// those are the ones Synapse has backed off furthest from — then the rest by name.
func destinationViews(all []synapse.Destination) ([]destinationView, int) {
	views := make([]destinationView, 0, len(all))
	failing := 0
	for _, d := range all {
		v := destinationView{Destination: d, Failing: d.FailureTS != nil}
		if v.Failing {
			failing++
			if d.RetryLastTS != nil && d.RetryInterval > 0 {
				v.NextRetryTS = *d.RetryLastTS + d.RetryInterval
			}
		}
		views = append(views, v)
	}
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Failing != b.Failing {
			return a.Failing
		}
		if a.Failing && *a.FailureTS != *b.FailureTS {
			return *a.FailureTS < *b.FailureTS
		}
		return a.Destination.Destination < b.Destination.Destination
	})
	return views, failing
}

// destinationParam reads the server name from the path. A server name is a host with
// an optional port — anything with a slash or of absurd length is not one.
func destinationParam(r *http.Request) (string, bool) {
	d, err := url.PathUnescape(chi.URLParam(r, "destination"))
	if err != nil || d == "" || len(d) > 255 || strings.ContainsAny(d, "/?#") {
		return "", false
	}
	return d, true
}

type destinationRoomView struct {
	RoomID string `json:"room_id"`
	Name   string `json:"name,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// roomNameLimit bounds the name lookups: one admin call per room, and a server shared
// with a large federation partner can share hundreds.
const roomNameLimit = 25

// GET /api/v1/federation/destinations/{destination}/rooms
func (h *FederationHandler) Rooms(w http.ResponseWriter, r *http.Request) {
	d, ok := destinationParam(r)
	if !ok {
		Error(w, http.StatusBadRequest, "kein gültiger Servername")
		return
	}
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	rooms, total, err := client.DestinationRooms(r.Context(), d, roomNameLimit)
	if err != nil {
		writeSynapseError(w, err, "Die gemeinsamen Räume konnten nicht geladen werden.")
		return
	}
	out := make([]destinationRoomView, len(rooms))
	var wg sync.WaitGroup
	for i, room := range rooms {
		out[i].RoomID = room.RoomID
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			// A name is a courtesy; a room whose details cannot be read is still listed.
			if det, err := client.GetRoom(r.Context(), id); err == nil && det != nil {
				out[i].Name, out[i].Alias = det.Name, det.CanonicalAlias
			}
		}(i, room.RoomID)
	}
	wg.Wait()
	JSON(w, http.StatusOK, map[string]any{"rooms": out, "total": total})
}

// POST /api/v1/federation/destinations/{destination}/reset — try now, not after the
// backoff, which after a long outage can be days.
func (h *FederationHandler) Reset(w http.ResponseWriter, r *http.Request) {
	d, ok := destinationParam(r)
	if !ok {
		Error(w, http.StatusBadRequest, "kein gültiger Servername")
		return
	}
	client := h.client(authmw.UserIDFromContext(r.Context()))
	if client == nil {
		Error(w, http.StatusServiceUnavailable, "Synapse ist nicht erreichbar")
		return
	}
	if err := client.ResetConnection(r.Context(), d); err != nil {
		writeSynapseError(w, err, "Die Verbindung konnte nicht zurückgesetzt werden.")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"destination": d})
}
