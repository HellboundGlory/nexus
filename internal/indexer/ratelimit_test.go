package indexer

import (
	"context"
	"testing"
	"time"
)

func TestLimiterEnforcesInterval(t *testing.T) {
	l := newLimiter(50 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	if err := l.wait(ctx); err != nil { // first call: immediate
		t.Fatal(err)
	}
	if err := l.wait(ctx); err != nil { // second call: waits ~50ms
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("second wait returned too early: %v", elapsed)
	}
}

func TestLimiterRespectsContext(t *testing.T) {
	l := newLimiter(time.Hour)
	ctx := context.Background()
	_ = l.wait(ctx) // consume the first immediate slot

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.wait(cctx); err == nil {
		t.Fatal("expected context error")
	}
}
