package downloadclient

import (
	"context"
	"sync"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
)

// DownloadStatusChanged is published for each new, changed, or removed queue item.
type DownloadStatusChanged struct {
	ClientID string                `json:"clientId"`
	Item     provider.DownloadItem `json:"item"`
	Removed  bool                  `json:"removed"`
}

func (DownloadStatusChanged) Name() string { return "download.status" }

// lastItem records the fields the monitor diffs on between runs.
type lastItem struct {
	clientID string
	status   provider.DownloadStatus
	progress float64
	item     provider.DownloadItem
}

// Monitor polls the aggregated queue and emits change events. It is stateful
// across runs, so the scheduler factory must return the SAME instance each tick.
type Monitor struct {
	svc *Service
	bus *events.Bus

	mu   sync.Mutex
	last map[string]lastItem
}

func NewMonitor(svc *Service, bus *events.Bus) *Monitor {
	return &Monitor{svc: svc, bus: bus, last: map[string]lastItem{}}
}

func (m *Monitor) Name() string { return "DownloadQueueMonitor" }

func (m *Monitor) Run(ctx context.Context, r command.Reporter) error {
	res := m.svc.Queue(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	current := make(map[string]lastItem, len(res.Items))
	for _, it := range res.Items {
		key := it.DownloadClientID + "|" + it.ID
		li := lastItem{clientID: it.DownloadClientID, status: it.Status, progress: it.Progress, item: it}
		current[key] = li
		prev, ok := m.last[key]
		if !ok || prev.status != it.Status || prev.progress != it.Progress {
			m.emit(ctx, DownloadStatusChanged{ClientID: it.DownloadClientID, Item: it})
		}
	}
	for key, prev := range m.last {
		if _, ok := current[key]; !ok {
			m.emit(ctx, DownloadStatusChanged{ClientID: prev.clientID, Item: prev.item, Removed: true})
		}
	}
	m.last = current
	r.Progress(100, "")
	return nil
}

func (m *Monitor) emit(ctx context.Context, e DownloadStatusChanged) {
	if m.bus != nil {
		m.bus.Publish(ctx, e)
	}
}
