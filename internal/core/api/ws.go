package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/events"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newHub() *hub { return &hub{clients: make(map[*websocket.Conn]struct{})} }

func (h *hub) add(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = c.Close()
}

func (h *hub) broadcast(m wsMessage) {
	payload, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
			h.remove(c)
		}
	}
}

func (s *server) registerWebSocket(r chi.Router) {
	if s.hub == nil {
		s.hub = newHub()
		// Subscribe once to bridge bus -> connected clients.
		if s.deps.Bus != nil {
			s.deps.Bus.Subscribe("task.updated", func(_ context.Context, e events.Event) {
				if tu, ok := e.(command.TaskUpdated); ok {
					s.hub.broadcast(wsMessage{Type: "task.updated", Data: tu.Task})
				}
			})
		}
	}
	r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		s.hub.add(conn)
		// Reader loop: discard client messages, detect close to clean up.
		go func() {
			defer s.hub.remove(conn)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	})
}
