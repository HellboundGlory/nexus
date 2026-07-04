package media

import (
	"context"
	"testing"
	"time"

	"github.com/hellboundg/nexus/internal/core/events"
)

// nopReporter satisfies command.Reporter (interface{ Progress(int, string) });
// mirrors internal/downloadclient/monitor_test.go — command has no exported Nop.
type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestRefreshCommandEmitsEvents(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	bus := events.New()
	got := make(chan string, 4)
	bus.Subscribe("media.series.updated", func(_ context.Context, e events.Event) { got <- e.Name() })
	svc = svc.WithBus(bus)
	ctx := context.Background()

	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != nil {
		t.Fatal(err)
	}
	// Drain the add event (PublishAsync → block briefly).
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("no event from AddSeries")
	}

	cmd := NewRefresh(svc)
	if cmd.Name() == "" {
		t.Fatal("command needs a name")
	}
	if err := cmd.Run(ctx, nopReporter{}); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-got:
		if name != "media.series.updated" {
			t.Fatalf("unexpected event %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh command did not emit a series-updated event")
	}
}
