// Package automation is Nexus's acquisition brain: it chooses releases for
// monitored library items and hands them to the import pipeline. It imports only
// internal/core/*, internal/parsing, internal/quality, and internal/importing;
// indexers are reached through the Searcher interface and the command manager
// through the Dispatcher interface, both wired at the composition root.
package automation

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

// Searcher runs an aggregated indexer search. Satisfied by an adapter over
// *indexer.Service that returns its releases plus a non-fatal aggregate error.
type Searcher interface {
	Search(ctx context.Context, q provider.Query) ([]provider.Release, error)
}

// Enqueuer decides+grabs a chosen release for a target item and records the
// tracking row. Satisfied by *importing.Service.
type Enqueuer interface {
	Enqueue(ctx context.Context, req importing.EnqueueRequest) (store.QueueItem, error)
}

// Service owns the search strategies and the missing-item sweep.
type Service struct {
	store   *store.Store
	search  Searcher
	enqueue Enqueuer
	bus     *events.Bus
}

func NewService(st *store.Store, search Searcher, enq Enqueuer, bus *events.Bus) *Service {
	return &Service{store: st, search: search, enqueue: enq, bus: bus}
}

func (s *Service) emit(ctx context.Context, ev events.Event) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, ev)
	}
}

// SearchCompleted is emitted when a search entrypoint finishes. ID is the target
// id of that entrypoint (movieId for movies; seriesId for series/season searches;
// episodeId for an episode search).
type SearchCompleted struct {
	Kind    string `json:"kind"` // "tv" or "movie"
	ID      int64  `json:"id"`
	Grabbed int    `json:"grabbed"`
}

func (SearchCompleted) Name() string { return "automation.search.completed" }
