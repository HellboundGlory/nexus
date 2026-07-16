package command

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingCmd runs until released, recording how many times it was entered.
type blockingCmd struct {
	runs    atomic.Int32
	started chan struct{}
	release chan struct{}
}

func newBlockingCmd() *blockingCmd {
	return &blockingCmd{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (c *blockingCmd) Name() string { return "Blocking" }

func (c *blockingCmd) Run(_ context.Context, _ Reporter) error {
	c.runs.Add(1)
	c.started <- struct{}{}
	<-c.release
	return nil
}

// A tick that fires while the previous run is still in flight must be skipped,
// not run concurrently: overlapping ImportCompleted runs can double-blocklist a
// failed release and double-grab its replacement.
func TestSingleFlightSkipsOverlappingRun(t *testing.T) {
	inner := newBlockingCmd()
	sf := Single(inner)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = sf.Run(context.Background(), nil)
	}()

	select {
	case <-inner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run never started")
	}

	// Second tick lands while the first is still in flight. It must return
	// promptly having done nothing — an unguarded delegate would block here.
	done := make(chan error, 1)
	go func() { done <- sf.Run(context.Background(), nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("skipped run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(inner.release)
		wg.Wait()
		t.Fatal("overlapping run was not skipped — it entered the inner command and blocked")
	}
	if got := inner.runs.Load(); got != 1 {
		t.Errorf("inner ran %d times during overlap, want 1", got)
	}

	close(inner.release)
	wg.Wait()
}

// The guard must reset so later ticks still run.
func TestSingleFlightRunsAgainAfterCompletion(t *testing.T) {
	inner := newBlockingCmd()
	close(inner.release) // never block
	sf := Single(inner)

	for i := 0; i < 2; i++ {
		if err := sf.Run(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		<-inner.started
	}
	if got := inner.runs.Load(); got != 2 {
		t.Errorf("inner ran %d times, want 2", got)
	}
}

func TestSingleFlightPreservesName(t *testing.T) {
	if got := Single(newBlockingCmd()).Name(); got != "Blocking" {
		t.Errorf("Name() = %q want %q", got, "Blocking")
	}
}
