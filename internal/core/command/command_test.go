package command

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type fakeCmd struct{ done chan struct{} }

func (fakeCmd) Name() string { return "Fake" }
func (f fakeCmd) Run(_ context.Context, r Reporter) error {
	r.Progress(50, "halfway")
	close(f.done)
	return nil
}

func newMgr(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	m := NewManager(s, events.New(), 2)
	return m, s
}

func TestEnqueueAfterStopReturnsError(t *testing.T) {
	m, _ := newMgr(t)
	m.Start()
	m.Stop()

	id, err := m.Enqueue(fakeCmd{done: make(chan struct{})})
	if err != ErrManagerStopped {
		t.Fatalf("expected ErrManagerStopped, got id=%q err=%v", id, err)
	}
}

func TestDoubleStopNoPanic(t *testing.T) {
	m, _ := newMgr(t)
	m.Start()
	m.Stop()
	m.Stop()
}

func TestEnqueueRunsAndCompletes(t *testing.T) {
	m, s := newMgr(t)
	m.Start()
	defer m.Stop()

	fc := fakeCmd{done: make(chan struct{})}
	id, err := m.Enqueue(fc)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("command never ran")
	}
	// Allow the worker to persist the terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for {
		task, err := s.GetTask(context.Background(), id)
		if err == nil && task.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not completed: %+v err=%v", task, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
