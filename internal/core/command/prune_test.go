package command

import (
	"context"
	"fmt"
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

type capReporter struct {
	pct int
	msg string
}

func (c *capReporter) Progress(pct int, msg string) { c.pct = pct; c.msg = msg }

func TestPruneCommandName(t *testing.T) {
	// newMgr (command_test.go) gives us a migrated *store.Store.
	_, s := newMgr(t)
	if got := NewPruneTasks(s, 50).Name(); got != "Housekeeping" {
		t.Fatalf("want Housekeeping, got %q", got)
	}
}

func TestPruneCommandRunPrunesAndReports(t *testing.T) {
	_, s := newMgr(t)
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		if err := s.UpsertTask(ctx, store.Task{ID: fmt.Sprintf("c%d", i), Name: "Job", Status: "completed"}); err != nil {
			t.Fatal(err)
		}
	}

	rep := &capReporter{}
	if err := NewPruneTasks(s, 50).Run(ctx, rep); err != nil {
		t.Fatal(err)
	}
	// 55 - 50 = 5 pruned.
	if rep.msg != "5 pruned" {
		t.Fatalf("want report %q, got %q", "5 pruned", rep.msg)
	}
	rows, err := s.ListTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 50 {
		t.Fatalf("want 50 rows kept, got %d", len(rows))
	}
}
