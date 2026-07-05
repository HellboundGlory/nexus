package automation

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
)

// SearchMovie searches for a monitored, file-less movie and enqueues the best
// acceptable release. Returns the number grabbed (0 or 1). Skips silently when
// the movie is unmonitored, already has a file, or has no quality profile.
func (s *Service) SearchMovie(ctx context.Context, movieID int64) (int, error) {
	n, err := s.searchMovie(ctx, movieID)
	s.emit(ctx, SearchCompleted{Kind: "movie", ID: movieID, Grabbed: n})
	return n, err
}

func (s *Service) searchMovie(ctx context.Context, movieID int64) (int, error) {
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		return 0, err
	}
	if !m.Monitored {
		return 0, nil
	}
	if f, err := s.store.MediaFileForMovie(ctx, m.ID); err != nil {
		return 0, err
	} else if f != nil {
		return 0, nil // already have a file
	}
	profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	releases, err := s.search.Search(ctx, movieQuery(m))
	if err != nil {
		slog.Warn("automation: movie search had indexer errors", "movieId", m.ID, "err", err)
	}
	cands := Decide(releases, provider.KindMovie, profile)
	grabbed, err := s.enqueueBest(ctx, cands, func(c Candidate) importing.EnqueueRequest {
		return importing.EnqueueRequest{
			DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
			Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
			MediaKind: provider.KindMovie, MovieID: m.ID,
		}
	})
	if err != nil {
		return 0, err
	}
	if grabbed {
		return 1, nil
	}
	return 0, nil
}

func movieQuery(m *store.Movie) provider.Query {
	q := provider.Query{Type: provider.SearchMovie, Kind: provider.KindMovie, Term: m.Title}
	if m.IMDbID != "" {
		q.IMDbID = m.IMDbID
	}
	if m.TMDBID != 0 {
		q.TMDBID = m.TMDBID
	}
	return q
}

// profileFor loads the assigned quality profile. ok=false (no error) means the
// item has no profile assigned and cannot be decided.
func (s *Service) profileFor(ctx context.Context, profileID *int64) (store.QualityProfile, bool, error) {
	if profileID == nil {
		return store.QualityProfile{}, false, nil
	}
	p, err := s.store.GetQualityProfile(ctx, *profileID)
	if err != nil {
		return store.QualityProfile{}, false, err
	}
	return p, true, nil
}

// enqueueBest tries candidates best-first, returning true once one is grabbed.
// A grab failure (network/reject) falls through to the next candidate;
// importing.ErrNoProfile is terminal for the item (nothing more to try).
func (s *Service) enqueueBest(ctx context.Context, cands []Candidate, req func(Candidate) importing.EnqueueRequest) (bool, error) {
	for _, c := range cands {
		if _, err := s.enqueue.Enqueue(ctx, req(c)); err == nil {
			return true, nil
		} else if errors.Is(err, importing.ErrNoProfile) {
			return false, nil
		} else {
			slog.Warn("automation: enqueue failed, trying next candidate", "title", c.Release.Title, "err", err)
		}
	}
	return false, nil
}
