package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

// wsHeartbeatInterval keeps the connection non-idle during Helm's silent phase.
// Traefik's default idleTimeout is 180 s; 20 s leaves room for a few lost beats
// before anything upstream considers the connection dead.
const wsHeartbeatInterval = 20 * time.Second

type WSHandler struct {
	helm *HelmHandler
	mu   sync.RWMutex
}

func NewWSHandler(helm *HelmHandler) *WSHandler {
	return &WSHandler{helm: helm}
}

func (h *WSHandler) UpgradeLogs(w http.ResponseWriter, r *http.Request) {
	upgradeID := chi.URLParam(r, "upgradeId")

	h.helm.mu.RLock()
	stream := h.helm.streams[upgradeID]
	h.helm.mu.RUnlock()

	if stream == nil {
		http.Error(w, "upgrade not found", http.StatusNotFound)
		return
	}

	wsHandler := websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()

		send := func(v any) bool {
			msg, _ := json.Marshal(v)
			_, err := ws.Write(msg)
			return err == nil
		}

		// sendProgress writes the current snapshot when it differs from the last one
		// sent on this connection.
		//
		// Pulled rather than pushed: progress is a *latest value*, not an event, so a
		// subscriber channel would only add a way to fall behind reality. Reading it
		// here also keeps the log path exactly as reliable as it was — the log is the
		// audit trail, and it must not start dropping lines because a progress
		// feature shares its buffer.
		var lastProgress string
		sendProgress := func() bool {
			p := stream.currentProgress()
			if p == nil {
				return true
			}
			body, err := json.Marshal(p)
			if err != nil || string(body) == lastProgress {
				return true
			}
			lastProgress = string(body)
			return send(map[string]any{"type": "progress", "progress": p})
		}

		// Replay what already happened, so a reconnecting client loses nothing.
		existing, isDone, doneStatus := stream.snapshot()
		for _, line := range existing {
			if !send(map[string]string{"type": "log", "line": line}) {
				return
			}
		}
		// After the replay and before the done check: a client reconnecting to a
		// still-running upgrade gets the current state immediately rather than after
		// the first tick.
		if !sendProgress() {
			return
		}
		if isDone {
			send(map[string]string{"type": "done", "status": doneStatus})
			return
		}

		// Subscribing and re-checking done in one locked step: without it an upgrade
		// finishing between the snapshot above and the subscription would close the
		// stream with nobody listening, and this connection would wait forever.
		ch, alreadyDone, status := stream.subscribe()
		if alreadyDone {
			send(map[string]string{"type": "done", "status": status})
			return
		}
		defer stream.unsubscribe(ch)

		// A heartbeat is required, not a nicety: helm runs with Wait=true and
		// produces no output for minutes, and Traefik closes an idle connection
		// after 180 s by default. golang.org/x/net/websocket has no ping/pong
		// frames, so the heartbeat is an ordinary message the client ignores —
		// which also survives proxies that would not forward control frames.
		heartbeat := time.NewTicker(wsHeartbeatInterval)
		defer heartbeat.Stop()

		progressTick := time.NewTicker(snapshotInterval)
		defer progressTick.Stop()

		for {
			select {
			case line, ok := <-ch:
				if !ok {
					// The last snapshot before "done", so the stepper lands on its
					// final step and the component table shows the finished state
					// rather than whatever it held three seconds ago.
					sendProgress()
					_, _, finalStatus := stream.snapshot()
					send(map[string]string{"type": "done", "status": finalStatus})
					return
				}
				if !send(map[string]string{"type": "log", "line": line}) {
					return
				}
			case <-progressTick.C:
				if !sendProgress() {
					return
				}
			case <-heartbeat.C:
				if !send(map[string]string{"type": "ping"}) {
					return
				}
			}
		}
	})

	wsHandler.ServeHTTP(w, r)
}
