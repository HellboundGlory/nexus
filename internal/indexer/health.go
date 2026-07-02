package indexer

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

// IndexerStatusChanged is published after each health evaluation.
type IndexerStatusChanged struct {
	IndexerID int64  `json:"indexerId"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (IndexerStatusChanged) Name() string { return "indexer.status" }

// HealthCheck pings every enabled indexer's caps endpoint and records status.
type HealthCheck struct {
	store *store.Store
	bus   *events.Bus
	http  *http.Client
}

func NewHealthCheck(st *store.Store, bus *events.Bus, hc *http.Client) *HealthCheck {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &HealthCheck{store: st, bus: bus, http: hc}
}

func (h *HealthCheck) Name() string { return "IndexerHealthCheck" }

func (h *HealthCheck) Run(ctx context.Context, r command.Reporter) error {
	rows, err := h.store.ListIndexers(ctx, true)
	if err != nil {
		return err
	}
	for i, ix := range rows {
		status, msg, capsJSON := "ok", "", ""
		caps, err := discoverCaps(ctx, h.http, ix.BaseURL, ix.APIKey)
		if err != nil {
			status, msg = "failed", err.Error()
		} else if b, mErr := json.Marshal(caps); mErr == nil {
			capsJSON = string(b)
		}
		if err := h.store.SetIndexerStatus(ctx, ix.ID, status, msg, capsJSON); err != nil {
			return err
		}
		if h.bus != nil {
			h.bus.Publish(ctx, IndexerStatusChanged{IndexerID: ix.ID, Status: status, Message: msg})
		}
		if len(rows) > 0 {
			r.Progress((i+1)*100/len(rows), ix.Name)
		}
	}
	return nil
}
