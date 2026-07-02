package provider

import (
	"context"
	"fmt"
	"sync"
)

type MediaKind string

const (
	KindTV    MediaKind = "tv"
	KindMovie MediaKind = "movie"
)

// Query is a minimal search request; extended by the indexer sub-project.
type Query struct {
	Term string
	Kind MediaKind
}

// Release is a minimal indexer result; extended by the indexer sub-project.
type Release struct {
	Title       string
	DownloadURL string
	Size        int64
	IndexerID   string
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
