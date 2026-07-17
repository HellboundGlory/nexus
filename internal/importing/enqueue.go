package importing

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// EnqueueRequest grabs one release for one library item (single episode, a set
// of episodes for a pack, or a movie) and records the tracking row.
type EnqueueRequest struct {
	DownloadURL string
	Title       string
	Protocol    provider.Protocol
	IndexerID   string
	ClientID    string // optional client override
	MediaKind   provider.MediaKind
	SeriesID    int64
	EpisodeIDs  []int64
	MovieID     int64
	// Force skips the quality accept gate for a release the user explicitly
	// picked in interactive search. It governs QUALITY ONLY: the blocklist is
	// not consulted on this path at all (filterBlocklisted lives in
	// automation.enqueueBest, not here), so force is a no-op for a blocklisted
	// release whose quality is fine. It never skips quality RESOLUTION — a
	// forced unparseable release still resolves to Unknown (ID 0).
	Force bool
}

// Enqueue decides the release against the item's profile, grabs it, and writes a
// grabbed queue row + history. Returns ErrRejected/ErrNoProfile without grabbing.
func (s *Service) Enqueue(ctx context.Context, req EnqueueRequest) (store.QueueItem, error) {
	profile, err := s.profileFor(ctx, req.MediaKind, req.SeriesID, req.MovieID)
	if err != nil {
		return store.QueueItem{}, err
	}
	parsed := parsing.Parse(req.Title, req.MediaKind)
	decision := quality.Decide(parsed, profile)
	if !decision.Accepted && !req.Force {
		return store.QueueItem{}, ErrRejected
	}
	itemID, err := s.grab.Grab(ctx, provider.DownloadRequest{
		URL: req.DownloadURL, Title: req.Title, Protocol: req.Protocol, IndexerID: req.IndexerID,
	}, req.ClientID)
	if err != nil {
		return store.QueueItem{}, err
	}
	row := store.QueueItem{
		DownloadClientID: req.ClientID, ClientItemID: itemID, Protocol: string(req.Protocol),
		SourceTitle: req.Title, MediaKind: string(req.MediaKind), EpisodeIDs: req.EpisodeIDs,
		QualityID: decision.Quality.ID, Status: store.QueueGrabbed,
	}
	if req.MediaKind == provider.KindTV {
		row.SeriesID = &req.SeriesID
	} else {
		row.MovieID = &req.MovieID
	}
	created, err := s.store.EnqueueGrab(ctx, row)
	if err != nil {
		return store.QueueItem{}, err
	}
	qid := decision.Quality.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "grabbed", MediaKind: string(req.MediaKind), SeriesID: created.SeriesID,
		MovieID: created.MovieID, SourceTitle: req.Title, QualityID: &qid,
	})
	s.emit(ctx, QueueUpdated{ID: created.ID})
	return created, nil
}

// profileFor loads the quality profile assigned to the target media item.
func (s *Service) profileFor(ctx context.Context, kind provider.MediaKind, seriesID, movieID int64) (store.QualityProfile, error) {
	var profileID *int64
	if kind == provider.KindTV {
		se, err := s.store.GetSeries(ctx, seriesID)
		if err != nil {
			return store.QualityProfile{}, err
		}
		profileID = se.QualityProfileID
	} else {
		m, err := s.store.GetMovie(ctx, movieID)
		if err != nil {
			return store.QualityProfile{}, err
		}
		profileID = m.QualityProfileID
	}
	if profileID == nil {
		return store.QualityProfile{}, ErrNoProfile
	}
	return s.store.GetQualityProfile(ctx, *profileID)
}
