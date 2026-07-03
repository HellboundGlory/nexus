package provider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type MediaKind string

const (
	KindTV    MediaKind = "tv"
	KindMovie MediaKind = "movie"
)

type SearchType string

const (
	SearchGeneric SearchType = "search"
	SearchTV      SearchType = "tvsearch"
	SearchMovie   SearchType = "movie"
)

type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// Query is a search request across indexers. Typed-search fields are used when
// Type is SearchTV or SearchMovie; they are ignored for SearchGeneric.
type Query struct {
	Type       SearchType
	Term       string
	Categories []int
	Season     *int
	Episode    *int
	IMDbID     string
	TVDBID     int
	TMDBID     int
	Limit      int
	Offset     int
	Kind       MediaKind
}

// Release is a single indexer result. Seeders/Leechers are set only for torrents.
type Release struct {
	Title       string
	DownloadURL string
	InfoURL     string
	Size        int64
	IndexerID   string
	Categories  []int
	PublishDate time.Time
	GUID        string
	Protocol    Protocol
	Seeders     *int
	Leechers    *int
}

// DownloadRequest is a minimal grab request; extended by the download sub-project.
type DownloadRequest struct {
	URL   string
	Title string
}

type Indexer interface {
	ID() string
	Search(ctx context.Context, q Query) ([]Release, error)
}

type DownloadClient interface {
	ID() string
	Add(ctx context.Context, d DownloadRequest) (string, error)
}

type MetadataProvider interface {
	ID() string
	Kind() MediaKind
}

// Registry is a concurrency-safe id→provider map.
type Registry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{items: make(map[string]T)}
}

func (r *Registry[T]) Register(id string, v T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[id]; exists {
		return fmt.Errorf("provider %q already registered", id)
	}
	r.items[id] = v
	return nil
}

func (r *Registry[T]) Get(id string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.items[id]
	return v, ok
}

func (r *Registry[T]) All() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, len(r.items))
	for _, v := range r.items {
		out = append(out, v)
	}
	return out
}
