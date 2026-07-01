package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

func TestWebSocketReceivesTaskUpdate(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	s := store.New(db)
	bus := events.New()

	d := Deps{Auth: auth.NewService(s, "k"), Store: s, Version: "test", Bus: bus}
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	srv := httptest.NewServer(NewRouter(d, spa))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
	header := http.Header{auth.APIKeyHeader: []string{"k"}}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Publish a task update through the bus.
	bus.PublishAsync(context.Background(), command.TaskUpdated{Task: store.Task{ID: "x", Name: "Y", Status: "running", Progress: 42}})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(msg), `"task.updated"`) || !strings.Contains(string(msg), `"progress":42`) {
		t.Fatalf("unexpected ws message: %s", msg)
	}
}
