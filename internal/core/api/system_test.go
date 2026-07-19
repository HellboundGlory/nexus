package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/scheduler"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeTasks struct{ ran string }

func (f *fakeTasks) Scheduled() []scheduler.ScheduledTask {
	return []scheduler.ScheduledTask{{Name: "Job", Interval: 5 * time.Second, NextRun: time.Now().Add(time.Minute)}}
}
func (f *fakeTasks) RunNow(name string) (string, error) {
	if name != "Job" {
		return "", fmt.Errorf("unknown task %q", name)
	}
	f.ran = name
	return "tid", nil
}

func tasksTestRouter(t *testing.T, ft *fakeTasks) (http.Handler, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	h := NewRouter(Deps{
		Auth: auth.NewService(st, "k"), Store: st, Version: "v", Bus: events.New(), Tasks: ft,
	}, http.NotFoundHandler())
	return h, st
}

func keyed(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set(auth.APIKeyHeader, "k")
	return req
}

func TestGetSystemTasks(t *testing.T) {
	ft := &fakeTasks{}
	h, st := tasksTestRouter(t, ft)
	ctx := context.Background()
	// a completed run of "Job" so lastExecution is populated
	_ = st.UpsertTask(ctx, store.Task{ID: "j1", Name: "Job", Status: "running"})
	_ = st.UpsertTask(ctx, store.Task{ID: "j1", Name: "Job", Status: "completed", Progress: 100})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodGet, "/api/v1/system/tasks"))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Scheduled []map[string]json.RawMessage `json:"scheduled"`
		Queue     []map[string]json.RawMessage `json:"queue"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scheduled) != 1 {
		t.Fatalf("want 1 scheduled, got %d", len(body.Scheduled))
	}
	// a never-run scheduled task would have null lastExecution; this one ran, so non-null
	if string(body.Scheduled[0]["lastExecution"]) == "null" {
		t.Fatal("lastExecution should be populated for a run task")
	}
	if len(body.Queue) != 1 {
		t.Fatalf("want 1 queue row, got %d", len(body.Queue))
	}
}

func TestRunSystemTask(t *testing.T) {
	ft := &fakeTasks{}
	h, _ := tasksTestRouter(t, ft)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodPost, "/api/v1/system/tasks/Job/run"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ft.ran != "Job" {
		t.Fatal("RunNow was not called for Job")
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, keyed(http.MethodPost, "/api/v1/system/tasks/Nope/run"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown task want 404, got %d", w.Code)
	}
}
