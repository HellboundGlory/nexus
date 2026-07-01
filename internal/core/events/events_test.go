package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testEvent struct{ v int }

func (testEvent) Name() string { return "test.event" }

func TestPublishSyncOrdered(t *testing.T) {
	b := New()
	var got []int
	b.Subscribe("test.event", func(_ context.Context, e Event) { got = append(got, 1) })
	b.Subscribe("test.event", func(_ context.Context, e Event) { got = append(got, 2) })
	b.Publish(context.Background(), testEvent{})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("handlers not run in order: %v", got)
	}
}

func TestPublishAsyncRecoversPanic(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	wg.Add(1)
	b.Subscribe("test.event", func(_ context.Context, e Event) { panic("boom") })
	b.Subscribe("test.event", func(_ context.Context, e Event) { defer wg.Done() })
	b.PublishAsync(context.Background(), testEvent{})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second handler did not run after first panicked")
	}
}
