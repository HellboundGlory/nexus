package indexer

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// NewznabClient is a single configured Newznab/Torznab indexer.
type NewznabClient struct {
	id       string
	name     string
	base     string
	apiKey   string
	protocol provider.Protocol
	priority int
	caps     Capabilities
	http     *http.Client
	lim      *limiter
}

func newClient(id, name, base, apiKey string, proto provider.Protocol, priority int,
	caps Capabilities, hc *http.Client, lim *limiter) *NewznabClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	if lim == nil {
		lim = newLimiter(0)
	}
	return &NewznabClient{
		id: id, name: name, base: base, apiKey: apiKey, protocol: proto,
		priority: priority, caps: caps, http: hc, lim: lim,
	}
}

func (c *NewznabClient) ID() string    { return c.id }
func (c *NewznabClient) Priority() int { return c.priority }

func (c *NewznabClient) Supports(q provider.Query) bool {
	t := q.Type
	if t == "" {
		t = provider.SearchGeneric
	}
	return c.caps.supports(t)
}

func (c *NewznabClient) Search(ctx context.Context, q provider.Query) ([]provider.Release, error) {
	if !c.Supports(q) {
		return nil, ErrUnsupportedSearch
	}
	if err := c.lim.wait(ctx); err != nil {
		return nil, err
	}
	raw, err := buildSearchURL(c.base, c.apiKey, q)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIndexerUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrAuthFailed
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrIndexerUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseReleases(body, c.id, c.protocol)
}
