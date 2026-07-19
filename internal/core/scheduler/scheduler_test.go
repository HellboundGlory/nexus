package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type countCmd struct{ n *int32 }

func (countCmd) Name() string { return "Count" }
func (c countCmd) Run(context.Context, command.Reporter) error {
	atomic.AddInt32(c.n, 1)
	return nil
}

func TestSchedulerDoubleStopNoPanic(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	m := command.NewManager(store.New(db), events.New(), 2)
	m.Start()
	defer m.Stop()

	sch := New(m)
	sch.Start()
	sch.Stop()
	sch.Stop()
}

func TestEveryFiresRepeatedly(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	m := command.NewManager(store.New(db), events.New(), 2)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(20*time.Millisecond, func() command.Command { return countCmd{n: &n} })
	sch.Start()
	defer sch.Stop()

	time.Sleep(120 * time.Millisecond)
	if atomic.LoadInt32(&n) < 2 {
		t.Fatalf("expected >=2 firings, got %d", n)
	}
}

func TestScheduledSnapshot(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	m := command.NewManager(store.New(db), events.New(), 1)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(time.Hour, func() command.Command { return countCmd{n: &n} })
	sch.Start()
	defer sch.Stop()

	got := sch.Scheduled()
	if len(got) != 1 {
		t.Fatalf("want 1 scheduled, got %d", len(got))
	}
	if got[0].Name != "Count" || got[0].Interval != time.Hour {
		t.Fatalf("bad entry: %+v", got[0])
	}
	if !got[0].NextRun.After(time.Now()) {
		t.Fatalf("next run should be in the future, got %v", got[0].NextRun)
	}
}

func TestRunNowEnqueues(t *testing.T) {
	db, _ := database.Open(t.TempDir() + "/t.db")
	defer db.Close()
	database.Migrate(db)
	st := store.New(db)
	m := command.NewManager(st, events.New(), 1)
	m.Start()
	defer m.Stop()

	var n int32
	sch := New(m)
	sch.Every(time.Hour, func() command.Command { return countCmd{n: &n} })
	// not started — RunNow should still enqueue immediately

	id, err := sch.RunNow("Count")
	if err != nil || id == "" {
		t.Fatalf("RunNow: id=%q err=%v", id, err)
	}
	if _, err := sch.RunNow("Nope"); !errors.Is(err, ErrNoSuchTask) {
		t.Fatalf("unknown task should return ErrNoSuchTask, got %v", err)
	}
	// the enqueued command should run and increment n
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&n) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&n) == 0 {
		t.Fatal("RunNow command did not execute")
	}
}
