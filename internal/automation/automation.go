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

// IndexerError names one indexer that failed during an aggregated search. It
// mirrors indexer.IndexerError's wire shape; automation declares its own copy
// because its package contract forbids importing internal/indexer (see the
// package doc above). The adapter at the composition root maps between them.
type IndexerError struct {
	IndexerID string `json:"indexerId"`
	Message   string `json:"message"`
}

// Searcher runs an aggregated indexer search. Satisfied by an adapter over
// *indexer.Service.
//
// Search returns the releases plus a non-fatal aggregate error, and is what the
// automatic paths (missing sweep, RSS, upgrade) use — they only need to log that
// something failed.
//
// SearchDetailed additionally names each failing indexer. Interactive search
// needs this: rendering a short list with no explanation of which indexers were
// missing reproduces exactly the invisibility that feature exists to remove. It
// returns no error because a partial result is the normal case — the caller
// surfaces the failures rather than aborting.
type Searcher interface {
	Search(ctx context.Context, q provider.Query) ([]provider.Release, error)
	SearchDetailed(ctx context.Context, q provider.Query) ([]provider.Release, []IndexerError)
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
