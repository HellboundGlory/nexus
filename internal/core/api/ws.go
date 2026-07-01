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

// client wraps a websocket connection with a buffered send channel. Only
// writePump ever calls conn.WriteMessage, satisfying gorilla/websocket's
// requirement that at most one goroutine write to a connection at a time.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

type hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func newHub() *hub { return &hub{clients: make(map[*client]struct{})} }

func (h *hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast(m wsMessage) {
	payload, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	for c := range h.clients {
		select {
		case c.send <- payload:
		default: // slow client: drop it to avoid blocking the broadcaster
			delete(h.clients, c)
			close(c.send)
		}
	}
	h.mu.Unlock()
}

// writePump is the ONLY goroutine that writes to c.conn.
func (c *client) writePump() {
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
	c.conn.Close()
}

func (s *server) registerWebSocket(r chi.Router) {
	if s.hub == nil {
		s.hub = newHub()
		// Subscribe once to bridge bus -> connected clients.
		// Guard against a nil Bus: some older Deps construction paths (and
		// tests) don't set one, so skip the subscription rather than panic.
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
		c := &client{conn: conn, send: make(chan []byte, 64)}
		s.hub.add(c)
		go c.writePump()
		// Reader loop: discard client messages, detect close to clean up.
		go func() {
			defer s.hub.remove(c)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	})
}
