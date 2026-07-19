// Package importing owns the grab-tracking download queue and the import
// pipeline: it attributes completed downloads to library items, decides
// accept/reject/upgrade, renames via templates, hardlinks files into root
// folders, tracks files, and records history. It imports only internal/core/*,
// internal/parsing, internal/quality, and internal/naming; download clients are
// reached via the Grabber/QueueReader interfaces wired at the composition root.
package importing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
)

// Grabber fetches a release and adds it to a download client, returning the
// client's item id. Satisfied by downloadclient.Service.Grab.
type Grabber interface {
	Grab(ctx context.Context, req provider.DownloadRequest, clientID string) (string, error)
}

// ClientError reports one download client that could not be reached during a
// queue read. Mirrors downloadclient.ClientError without importing that package.
type ClientError struct {
	ClientID string `json:"clientId"`
	Message  string `json:"message"`
}

// QueueSnapshot is one read of the aggregated download-client queue. Items and
// ClientErrors are both partial: a client that failed contributes an entry to
// ClientErrors and no items. Callers that only need the items read .Items;
// callers that must not act on an incomplete picture check .ClientErrors.
type QueueSnapshot struct {
	Items        []provider.DownloadItem
	ClientErrors []ClientError
}

// QueueReader reads the aggregated download-client queue and removes items.
// Satisfied by a thin adapter over downloadclient.Service at the composition root.
type QueueReader interface {
	Queue(ctx context.Context) QueueSnapshot
	Remove(ctx context.Context, clientID, itemID string, deleteData bool) error
}

const namingSettingKey = "naming.config"

// Service owns enqueue + import.
type Service struct {
	store      *store.Store
	grab       Grabber
	queue      QueueReader
	bus        *events.Bus
	researcher Researcher
}

func NewService(st *store.Store, grab Grabber, q QueueReader, bus *events.Bus) *Service {
	return &Service{store: st, grab: grab, queue: q, bus: bus}
}

// Researcher re-searches a target after its download failed. Implemented by the
// automation service and wired in main.go (importing defines the interface to
// avoid an import cycle: automation depends on importing, not the reverse).
type Researcher interface {
	ResearchMovie(ctx context.Context, movieID int64) error
	ResearchEpisode(ctx context.Context, episodeID int64) error
}

func (s *Service) SetResearcher(r Researcher) { s.researcher = r }

// NamingConfig returns the persisted config, or the defaults if none saved.
func (s *Service) NamingConfig(ctx context.Context) (naming.Config, error) {
	raw, ok, err := s.store.GetSetting(ctx, namingSettingKey)
	if err != nil {
		return naming.Config{}, err
	}
	if !ok {
		return naming.DefaultConfig(), nil
	}
	var c naming.Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return naming.Config{}, err
	}
	return c, nil
}

// SetNamingConfig persists the naming config.
func (s *Service) SetNamingConfig(ctx context.Context, c naming.Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, namingSettingKey, string(b))
}

func (s *Service) emit(ctx context.Context, ev events.Event) {
	if s.bus != nil {
		s.bus.PublishAsync(ctx, ev)
	}
}

// QueueUpdated is emitted when a queue row changes.
type QueueUpdated struct {
	ID int64 `json:"id"`
}

func (QueueUpdated) Name() string { return "queue.updated" }

// ImportCompletedEvent is emitted after an import attempt on a row.
type ImportCompletedEvent struct {
	QueueID int64  `json:"queueId"`
	Status  string `json:"status"`
}

func (ImportCompletedEvent) Name() string { return "import.completed" }

// DownloadFailedEvent is published when a grabbed download failed and was
// blocklisted + removed. Forwarded to the UI so it refreshes queue/history/blocklist.
type DownloadFailedEvent struct {
	QueueID    int64   `json:"queueId"`
	MediaKind  string  `json:"mediaKind"`
	MovieID    *int64  `json:"movieId,omitempty"`
	SeriesID   *int64  `json:"seriesId,omitempty"`
	EpisodeIDs []int64 `json:"episodeIds"`
}

func (DownloadFailedEvent) Name() string { return "download.failed" }

var (
	// ErrRejected means the release's quality is not allowed by the item's profile.
	ErrRejected = errors.New("importing: release rejected by quality profile")
	// ErrNoProfile means the target media item has no quality profile assigned.
	ErrNoProfile = errors.New("importing: media item has no quality profile")
)
