package indexer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
)

type searchable interface {
	provider.Indexer
	Priority() int
	Supports(provider.Query) bool
}

type IndexerError struct {
	IndexerID string `json:"indexerId"`
	Message   string `json:"message"`
}

type SearchResult struct {
	Releases      []provider.Release `json:"releases"`
	IndexerErrors []IndexerError     `json:"indexerErrors"`
}

func searchAll(ctx context.Context, clients []searchable, q provider.Query, perTimeout time.Duration) SearchResult {
	type outcome struct {
		id       string
		priority int
		releases []provider.Release
		err      error
	}

	var wg sync.WaitGroup
	results := make([]outcome, len(clients))
	for i, c := range clients {
		if !c.Supports(q) {
			results[i] = outcome{id: c.ID(), priority: c.Priority()} // skipped, no error
			continue
		}
		wg.Add(1)
		go func(i int, c searchable) {
			defer wg.Done()
			cctx := ctx
			var cancel context.CancelFunc
			if perTimeout > 0 {
				cctx, cancel = context.WithTimeout(ctx, perTimeout)
				defer cancel()
			}
			rels, err := c.Search(cctx, q)
			results[i] = outcome{id: c.ID(), priority: c.Priority(), releases: rels, err: err}
		}(i, c)
	}
	wg.Wait()

	// Process in ascending priority so higher-priority indexers win dedupe ties.
	order := make([]int, len(results))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return results[order[a]].priority < results[order[b]].priority
	})

	priorityByID := make(map[string]int, len(results))
	seen := make(map[string]struct{})
	var out SearchResult
	for _, idx := range order {
		o := results[idx]
		priorityByID[o.id] = o.priority
		if o.err != nil {
			out.IndexerErrors = append(out.IndexerErrors, IndexerError{IndexerID: o.id, Message: o.err.Error()})
			continue
		}
		for _, r := range o.releases {
			key := dedupeKey(r)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out.Releases = append(out.Releases, r)
		}
	}

	sort.SliceStable(out.Releases, func(a, b int) bool {
		ra, rb := out.Releases[a], out.Releases[b]
		if !ra.PublishDate.Equal(rb.PublishDate) {
			return ra.PublishDate.After(rb.PublishDate)
		}
		return priorityByID[ra.IndexerID] < priorityByID[rb.IndexerID]
	})
	return out
}

func dedupeKey(r provider.Release) string {
	if r.GUID != "" {
		return string(r.Protocol) + "|guid|" + r.GUID
	}
	return string(r.Protocol) + "|ts|" + strings.ToLower(strings.TrimSpace(r.Title)) + "|" + fmt.Sprint(r.Size)
}
