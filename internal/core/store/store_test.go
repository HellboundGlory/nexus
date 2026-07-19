package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetSetting(ctx, "x"); ok {
		t.Fatal("expected missing key")
	}
	if err := s.SetSetting(ctx, "x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "x", "2"); err != nil { // upsert
		t.Fatal(err)
	}
	v, ok, err := s.GetSetting(ctx, "x")
	if err != nil || !ok || v != "2" {
		t.Fatalf("got %q ok=%v err=%v", v, ok, err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if n, _ := s.CountUsers(ctx); n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}
	id, err := s.CreateUser(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUserByUsername(ctx, "admin")
	if err != nil || u.ID != id || u.PasswordHash != "hash" {
		t.Fatalf("bad user: %+v err=%v", u, err)
	}
	tok := "tok123"
	if err := s.CreateSession(ctx, tok, id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession(ctx, tok)
	if err != nil || sess.UserID != id {
		t.Fatalf("bad session: %+v err=%v", sess, err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, tok); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTasksUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := Task{ID: "a", Name: "Test", Status: "queued"}
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task.Status = "completed"
	task.Progress = 100
	if err := s.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, "a")
	if err != nil || got.Status != "completed" || got.Progress != 100 {
		t.Fatalf("bad task: %+v err=%v", got, err)
	}
}

func TestTaskTimestampsLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// queued: no started/ended yet
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	q, _ := st.GetTask(ctx, "t1")
	if q.StartedAt != nil || q.EndedAt != nil {
		t.Fatalf("queued task should have nil started/ended, got %+v", q)
	}

	// running: started set, ended still nil
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	r, _ := st.GetTask(ctx, "t1")
	if r.StartedAt == nil || r.EndedAt != nil {
		t.Fatalf("running task should have started set, ended nil, got %+v", r)
	}

	// completed: ended set, started preserved
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "completed", Progress: 100}); err != nil {
		t.Fatal(err)
	}
	c, _ := st.GetTask(ctx, "t1")
	if c.StartedAt == nil || c.EndedAt == nil {
		t.Fatalf("completed task should have both timestamps, got %+v", c)
	}
	if !c.StartedAt.Equal(*r.StartedAt) {
		t.Fatal("started_at must be preserved across the terminal transition")
	}
}

func TestTaskTimestampsFirstInsertRunning(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// First-ever UpsertTask for this id goes straight to the INSERT branch
	// (no prior queued row); status "running" must still derive started_at.
	if err := st.UpsertTask(ctx, Task{ID: "t1", Name: "Job", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTask(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.StartedAt == nil {
		t.Fatal("first-ever running insert should set started_at, got nil")
	}
	if got.EndedAt != nil {
		t.Fatalf("first-ever running insert should leave ended_at nil, got %+v", got.EndedAt)
	}
}

func TestLastTaskByName(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if got, err := st.LastTaskByName(ctx, "Nope"); err != nil || got != nil {
		t.Fatalf("want nil,nil for unknown name, got %v,%v", got, err)
	}
	_ = st.UpsertTask(ctx, Task{ID: "a", Name: "Job", Status: "completed"})
	_ = st.UpsertTask(ctx, Task{ID: "b", Name: "Job", Status: "completed"})
	last, err := st.LastTaskByName(ctx, "Job")
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.Name != "Job" {
		t.Fatalf("want a Job task, got %+v", last)
	}
}

func TestPruneTasksPerName(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// A frequent task with 60 terminal rows, plus a queued and a running row.
	for i := 0; i < 60; i++ {
		if err := st.UpsertTask(ctx, Task{ID: fmt.Sprintf("f%d", i), Name: "Frequent", Status: "completed"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertTask(ctx, Task{ID: "fq", Name: "Frequent", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTask(ctx, Task{ID: "fr", Name: "Frequent", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	// An infrequent task with a SINGLE old terminal row — must survive.
	if err := st.UpsertTask(ctx, Task{ID: "r0", Name: "Rare", Status: "completed"}); err != nil {
		t.Fatal(err)
	}

	deleted, err := st.PruneTasksPerName(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	// 60 Frequent completed - 50 kept = 10 deleted; Rare/queued/running untouched.
	if deleted != 10 {
		t.Fatalf("want 10 deleted, got %d", deleted)
	}

	rows, err := st.ListTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{} // "name/status" -> count
	for _, r := range rows {
		counts[r.Name+"/"+r.Status]++
	}
	if counts["Frequent/completed"] != 50 {
		t.Fatalf("Frequent completed should be capped at 50, got %d", counts["Frequent/completed"])
	}
	if counts["Rare/completed"] != 1 {
		t.Fatalf("Rare's lone terminal row must survive, got %d", counts["Rare/completed"])
	}
	if counts["Frequent/queued"] != 1 || counts["Frequent/running"] != 1 {
		t.Fatalf("queued/running must never be pruned, got queued=%d running=%d",
			counts["Frequent/queued"], counts["Frequent/running"])
	}
}

func TestPruneTasksPerNameBelowThreshold(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = st.UpsertTask(ctx, Task{ID: fmt.Sprintf("x%d", i), Name: "X", Status: "completed"})
	}
	deleted, err := st.PruneTasksPerName(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("below threshold should delete nothing, got %d", deleted)
	}
}

func TestCountActiveTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.UpsertTask(ctx, Task{ID: "a", Name: "N", Status: "queued"})
	_ = st.UpsertTask(ctx, Task{ID: "b", Name: "N", Status: "running"})
	_ = st.UpsertTask(ctx, Task{ID: "c", Name: "N", Status: "completed"})
	_ = st.UpsertTask(ctx, Task{ID: "d", Name: "N", Status: "failed"})

	n, err := st.CountActiveTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 active (queued+running), got %d", n)
	}
}
