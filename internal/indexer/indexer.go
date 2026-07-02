// Package indexer implements Prowlarr-equivalent indexer management and search
// over the Newznab and Torznab protocols. It fills the provider.Indexer contract
// declared in core/provider and imports only internal/core/*.
package indexer

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

const (
	defaultRateInterval  = 2 * time.Second
	defaultSearchTimeout = 30 * time.Second
)

// Service owns the live set of configured indexer clients and runs searches.
type Service struct {
	store         *store.Store
	http          *http.Client
	rateInterval  time.Duration
	searchTimeout time.Duration

	mu      sync.RWMutex
	clients []searchable
}

func NewService(st *store.Store) *Service {
	return &Service{
		store:         st,
		http:          &http.Client{Timeout: 60 * time.Second},
		rateInterval:  defaultRateInterval,
		searchTimeout: defaultSearchTimeout,
	}
}

func (s *Service) WithHTTPClient(hc *http.Client) *Service {
	s.http = hc
	return s
}

// Reload rebuilds the live client set from enabled indexers in the store.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.store.ListIndexers(ctx, true)
	if err != nil {
		return err
	}
	clients := make([]searchable, 0, len(rows))
	for _, ix := range rows {
		var caps Capabilities
		if ix.Caps != "" {
			_ = json.Unmarshal([]byte(ix.Caps), &caps)
		} else {
			// No caps discovered yet: default to permissive (all search types) so a
			// freshly-added indexer is usable immediately; the health check refines
			// this once real caps are fetched.
			caps = Capabilities{Search: true, TVSearch: true, MovieSearch: true}
		}
		proto := provider.ProtocolUsenet
		if ix.Implementation == "torznab" {
			proto = provider.ProtocolTorrent
		}
		clients = append(clients, newClient(
			strconv.FormatInt(ix.ID, 10), ix.Name, ix.BaseURL, ix.APIKey,
			proto, ix.Priority, caps, s.http, newLimiter(s.rateInterval),
		))
	}
	s.mu.Lock()
	s.clients = clients
	s.mu.Unlock()
	return nil
}

func (s *Service) snapshot() []searchable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]searchable, len(s.clients))
	copy(out, s.clients)
	return out
}

func (s *Service) Search(ctx context.Context, q provider.Query) SearchResult {
	return searchAll(ctx, s.snapshot(), q, s.searchTimeout)
}
