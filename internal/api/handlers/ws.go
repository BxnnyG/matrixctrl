package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

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

		send := func(v map[string]string) bool {
			msg, _ := json.Marshal(v)
			_, err := ws.Write(msg)
			return err == nil
		}

		// Send existing logs first
		stream.mu.Lock()
		existing := make([]string, len(stream.logs))
		copy(existing, stream.logs)
		isDone := stream.done
		doneStatus := stream.status
		stream.mu.Unlock()

		for _, line := range existing {
			if !send(map[string]string{"type": "log", "line": line}) {
				return
			}
		}

		if isDone {
			send(map[string]string{"type": "done", "status": doneStatus})
			return
		}

		// Subscribe to future logs
		ch := make(chan string, 64)
		stream.mu.Lock()
		stream.subs = append(stream.subs, ch)
		stream.mu.Unlock()

		for line := range ch {
			if !send(map[string]string{"type": "log", "line": line}) {
				return
			}
		}

		send(map[string]string{"type": "done", "status": stream.status})
	})

	wsHandler.ServeHTTP(w, r)
}
