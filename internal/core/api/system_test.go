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
	return []scheduler.ScheduledTask{
		{Name: "Job", Interval: 5 * time.Second, NextRun: time.Now().Add(time.Minute)},
		// no task rows exist for this name -> exercises the null-emission branch
		{Name: "NeverRun", Interval: 10 * time.Second, NextRun: time.Now().Add(2 * time.Minute)},
	}
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
	// a queue row that was inserted but never transitioned to running/completed,
	// so started_at/ended_at stay NULL -> exercises the queue-row null branch
	_ = st.UpsertTask(ctx, store.Task{ID: "q1", Name: "Job", Status: "queued"})

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
	if len(body.Scheduled) != 2 {
		t.Fatalf("want 2 scheduled, got %d", len(body.Scheduled))
	}

	var job, neverRun map[string]json.RawMessage
	for _, sc := range body.Scheduled {
		var name string
		if err := json.Unmarshal(sc["name"], &name); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "Job":
			job = sc
		case "NeverRun":
			neverRun = sc
		}
	}
	if job == nil {
		t.Fatal("missing \"Job\" scheduled entry")
	}
	if neverRun == nil {
		t.Fatal("missing \"NeverRun\" scheduled entry")
	}
	// this one ran, so non-null
	if string(job["lastExecution"]) == "null" {
		t.Fatal("lastExecution should be populated for a run task")
	}
	// a never-run scheduled task has no task rows -> null, never 0
	if string(neverRun["lastExecution"]) != "null" {
		t.Fatalf("NeverRun lastExecution should be null, got %s", neverRun["lastExecution"])
	}
	if string(neverRun["lastDurationSeconds"]) != "null" {
		t.Fatalf("NeverRun lastDurationSeconds should be null, got %s", neverRun["lastDurationSeconds"])
	}

	if len(body.Queue) != 2 {
		t.Fatalf("want 2 queue rows, got %d", len(body.Queue))
	}
	var completedRow, queuedRow map[string]json.RawMessage
	for _, q := range body.Queue {
		var id string
		if err := json.Unmarshal(q["id"], &id); err != nil {
			t.Fatal(err)
		}
		switch id {
		case "j1":
			completedRow = q
		case "q1":
			queuedRow = q
		}
	}
	if completedRow == nil {
		t.Fatal("missing \"j1\" queue row")
	}
	if queuedRow == nil {
		t.Fatal("missing \"q1\" queue row")
	}
	// never-started queue row -> started_at/ended_at/durationSeconds null, never 0
	if string(queuedRow["startedAt"]) != "null" {
		t.Fatalf("queued row startedAt should be null, got %s", queuedRow["startedAt"])
	}
	if string(queuedRow["endedAt"]) != "null" {
		t.Fatalf("queued row endedAt should be null, got %s", queuedRow["endedAt"])
	}
	if string(queuedRow["durationSeconds"]) != "null" {
		t.Fatalf("queued row durationSeconds should be null, got %s", queuedRow["durationSeconds"])
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
