// Package downloadclient integrates usenet (SABnzbd) and torrent (qBittorrent)
// download clients. It fills the provider.DownloadClient contract declared in
// core/provider and imports only internal/core/*. Releases are fetched
// server-side so the indexer apikey never leaves Nexus; magnets pass through.
package downloadclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ClientError is a per-client failure captured during queue fan-out.
type ClientError struct {
	ClientID string `json:"clientId"`
	Message  string `json:"message"`
}

// QueueResult is the aggregated live queue across enabled clients.
type QueueResult struct {
	Items        []provider.DownloadItem `json:"items"`
	ClientErrors []ClientError           `json:"clientErrors"`
}

// Service owns the live set of configured download clients.
type Service struct {
	store *store.Store
	http  *http.Client

	mu      sync.RWMutex
	clients []provider.DownloadClient // priority order preserved from store
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, http: &http.Client{Timeout: 30 * time.Second}}
}

func (s *Service) WithHTTPClient(hc *http.Client) *Service {
	s.http = hc
	return s
}

// Reload rebuilds the live client set from enabled clients in the store,
// preserving the store's priority ordering.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.store.ListDownloadClients(ctx, true)
	if err != nil {
		return err
	}
	clients := make([]provider.DownloadClient, 0, len(rows))
	for _, dc := range rows {
		id := strconv.FormatInt(dc.ID, 10)
		base := buildBase(dc)
		switch dc.Implementation {
		case "sabnzbd":
			clients = append(clients, newSABnzbd(id, base, dc.APIKey, dc.Category, s.http))
		case "qbittorrent":
			clients = append(clients, newQBittorrent(id, base, dc.Username, dc.APIKey, dc.Category, s.http))
		}
	}
	s.mu.Lock()
	s.clients = clients
	s.mu.Unlock()
	return nil
}

// buildBase composes scheme://host:port + url base from a stored config.
func buildBase(dc store.DownloadClient) string {
	scheme := "http"
	if dc.UseSSL {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, dc.Host)
	if dc.Port != 0 {
		base = fmt.Sprintf("%s:%d", base, dc.Port)
	}
	if dc.URLBase != "" {
		if dc.URLBase[0] != '/' {
			base += "/"
		}
		base += dc.URLBase
	}
	return base
}

func (s *Service) snapshot() []provider.DownloadClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]provider.DownloadClient, len(s.clients))
	copy(out, s.clients)
	return out
}

// route selects the target client: an explicit clientID wins; otherwise the
// highest-priority enabled client whose protocol matches.
func (s *Service) route(protocol provider.Protocol, clientID string) (provider.DownloadClient, error) {
	clients := s.snapshot()
	if clientID != "" {
		for _, c := range clients {
			if c.ID() == clientID {
				return c, nil
			}
		}
		return nil, fmt.Errorf("%w: client %q not found or disabled", ErrClientUnavailable, clientID)
	}
	for _, c := range clients { // snapshot is priority-ordered
		if c.Protocol() == protocol {
			return c, nil
		}
	}
	return nil, ErrUnsupportedProtocol
}

// Grab routes a release, fetches its content server-side, and submits it.
func (s *Service) Grab(ctx context.Context, req provider.DownloadRequest, clientID string) (string, error) {
	c, err := s.route(req.Protocol, clientID)
	if err != nil {
		return "", err
	}
	content, err := fetchContent(ctx, s.http, req.URL)
	if err != nil {
		return "", err
	}
	req.Content = content
	return c.Add(ctx, req)
}

// Queue fans out Items() across enabled clients with partial-success semantics.
func (s *Service) Queue(ctx context.Context) QueueResult {
	clients := s.snapshot()
	type outcome struct {
		id    string
		items []provider.DownloadItem
		err   error
	}
	var wg sync.WaitGroup
	results := make([]outcome, len(clients))
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c provider.DownloadClient) {
			defer wg.Done()
			items, err := c.Items(ctx)
			results[i] = outcome{id: c.ID(), items: items, err: err}
		}(i, c)
	}
	wg.Wait()

	var out QueueResult
	for _, o := range results {
		if o.err != nil {
			out.ClientErrors = append(out.ClientErrors, ClientError{ClientID: o.id, Message: o.err.Error()})
			continue
		}
		out.Items = append(out.Items, o.items...)
	}
	return out
}

// Remove deletes a queue item from a named client.
func (s *Service) Remove(ctx context.Context, clientID, itemID string, deleteData bool) error {
	for _, c := range s.snapshot() {
		if c.ID() == clientID {
			return c.Remove(ctx, itemID, deleteData)
		}
	}
	return fmt.Errorf("%w: client %q not found or disabled", ErrClientUnavailable, clientID)
}
