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

type DownloadStatus string

const (
	StatusQueued      DownloadStatus = "queued"
	StatusDownloading DownloadStatus = "downloading"
	StatusCompleted   DownloadStatus = "completed"
	StatusPaused      DownloadStatus = "paused"
	StatusFailed      DownloadStatus = "failed"
	StatusWarning     DownloadStatus = "warning"
)

// DownloadItem is one entry in a client's queue/history snapshot.
type DownloadItem struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Status           DownloadStatus `json:"status"`
	Progress         float64        `json:"progress"` // 0..100
	Size             int64          `json:"size"`
	Downloaded       int64          `json:"downloaded"`
	DownloadClientID string         `json:"downloadClientId"`
	Protocol         Protocol       `json:"protocol"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
}

// DownloadRequest is a grab. Content holds pre-fetched .nzb/.torrent bytes; it is
// nil for magnet links (URL is passed through to the client).
type DownloadRequest struct {
	URL       string
	Title     string
	Protocol  Protocol
	IndexerID string
	Category  string
	Content   []byte
}

type Indexer interface {
	ID() string
	Search(ctx context.Context, q Query) ([]Release, error)
}

type DownloadClient interface {
	ID() string
	Protocol() Protocol
	Add(ctx context.Context, d DownloadRequest) (string, error) // returns client item id
	Items(ctx context.Context) ([]DownloadItem, error)
	Remove(ctx context.Context, id string, deleteData bool) error
	Test(ctx context.Context) error
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
