package scheduler

import (
	"context"
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
