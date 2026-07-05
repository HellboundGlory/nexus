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

// SearchSeries searches every monitored season of a monitored series for its
// missing episodes. Returns the total grabbed.
func (s *Service) SearchSeries(ctx context.Context, seriesID int64) (int, error) {
	n, err := s.searchSeries(ctx, seriesID)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: seriesID, Grabbed: n})
	return n, err
}

func (s *Service) searchSeries(ctx context.Context, seriesID int64) (int, error) {
	se, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	if !se.Monitored {
		return 0, nil
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	seasons, err := s.store.ListSeasons(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	eps, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, sn := range seasons {
		if !sn.Monitored {
			continue
		}
		n, err := s.searchSeason(ctx, se, sn.SeasonNumber, eps, profile)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// SearchSeason searches one season of a series.
func (s *Service) SearchSeason(ctx context.Context, seriesID int64, seasonNumber int) (int, error) {
	n, err := s.searchSeasonEntry(ctx, seriesID, seasonNumber)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: seriesID, Grabbed: n})
	return n, err
}

func (s *Service) searchSeasonEntry(ctx context.Context, seriesID int64, seasonNumber int) (int, error) {
	se, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	if !se.Monitored {
		return 0, nil
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	eps, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	return s.searchSeason(ctx, se, seasonNumber, eps, profile)
}

// searchSeason searches a single season. If every monitored episode in the
// season is missing, it tries a full-season pack first (enqueued with all
// missing episode ids); otherwise, or if no acceptable pack is found, it falls
// back to per-episode searches. eps is the full episode list for the series.
func (s *Service) searchSeason(ctx context.Context, se *store.Series, seasonNumber int, eps []store.Episode, profile store.QualityProfile) (int, error) {
	var monitored, missing []store.Episode
	for _, e := range eps {
		if e.SeasonNumber != seasonNumber || !e.Monitored {
			continue
		}
		monitored = append(monitored, e)
		f, err := s.store.MediaFileForEpisode(ctx, e.ID)
		if err != nil {
			return 0, err
		}
		if f == nil {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	// Fully missing → try a season pack first.
	if len(missing) == len(monitored) {
		releases, err := s.search.Search(ctx, tvQuery(se, seasonNumber, nil))
		if err != nil {
			slog.Warn("automation: season search had indexer errors", "seriesId", se.ID, "season", seasonNumber, "err", err)
		}
		var packs []Candidate
		for _, c := range Decide(releases, provider.KindTV, profile) {
			if c.Parsed.Season == seasonNumber && len(c.Parsed.Episodes) == 0 {
				packs = append(packs, c)
			}
		}
		ids := episodeIDs(missing)
		grabbed, err := s.enqueueBest(ctx, packs, func(c Candidate) importing.EnqueueRequest {
			return tvRequest(se.ID, ids, c)
		})
		if err != nil {
			return 0, err
		}
		if grabbed {
			return 1, nil
		}
		// no acceptable pack → fall through to per-episode
	}
	total := 0
	for _, e := range missing {
		n, err := s.searchEpisode(ctx, se, e, profile)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// SearchEpisode searches for one missing, monitored episode.
func (s *Service) SearchEpisode(ctx context.Context, episodeID int64) (int, error) {
	n, err := s.searchEpisodeEntry(ctx, episodeID)
	s.emit(ctx, SearchCompleted{Kind: "tv", ID: episodeID, Grabbed: n})
	return n, err
}

func (s *Service) searchEpisodeEntry(ctx context.Context, episodeID int64) (int, error) {
	e, err := s.store.GetEpisode(ctx, episodeID)
	if err != nil {
		return 0, err
	}
	se, err := s.store.GetSeries(ctx, e.SeriesID)
	if err != nil {
		return 0, err
	}
	if !se.Monitored || !e.Monitored {
		return 0, nil
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil || !ok {
		return 0, err
	}
	return s.searchEpisode(ctx, se, *e, profile)
}

// searchEpisode searches one episode and enqueues the best covering release.
// Skips if the episode already has a file.
func (s *Service) searchEpisode(ctx context.Context, se *store.Series, e store.Episode, profile store.QualityProfile) (int, error) {
	f, err := s.store.MediaFileForEpisode(ctx, e.ID)
	if err != nil {
		return 0, err
	}
	if f != nil {
		return 0, nil
	}
	ep := e.EpisodeNumber
	releases, err := s.search.Search(ctx, tvQuery(se, e.SeasonNumber, &ep))
	if err != nil {
		slog.Warn("automation: episode search had indexer errors", "episodeId", e.ID, "err", err)
	}
	var covering []Candidate
	for _, c := range Decide(releases, provider.KindTV, profile) {
		if c.Parsed.Season == e.SeasonNumber && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
			covering = append(covering, c)
		}
	}
	grabbed, err := s.enqueueBest(ctx, covering, func(c Candidate) importing.EnqueueRequest {
		return tvRequest(se.ID, []int64{e.ID}, c)
	})
	if err != nil {
		return 0, err
	}
	if grabbed {
		return 1, nil
	}
	return 0, nil
}

func tvQuery(se *store.Series, season int, episode *int) provider.Query {
	q := provider.Query{Type: provider.SearchTV, Kind: provider.KindTV, Term: se.Title, Season: &season}
	if se.TMDBID != 0 {
		q.TMDBID = se.TMDBID
	}
	if episode != nil {
		q.Episode = episode
	}
	return q
}

func tvRequest(seriesID int64, episodeIDs []int64, c Candidate) importing.EnqueueRequest {
	return importing.EnqueueRequest{
		DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
		Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
		MediaKind: provider.KindTV, SeriesID: seriesID, EpisodeIDs: episodeIDs,
	}
}

func episodeIDs(eps []store.Episode) []int64 {
	out := make([]int64, len(eps))
	for i, e := range eps {
		out[i] = e.ID
	}
	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// MissingSweep processes up to batch monitored targets that are missing files:
// first monitored movies without a file, then monitored series (each of which
// may fan out to several episode searches). Returns the total grabbed. A per-
// target error is not fatal to the sweep — it is logged and the sweep continues.
func (s *Service) MissingSweep(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = DefaultConfig().MissingSearchBatchSize
	}
	processed, total := 0, 0

	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range movies {
		if processed >= batch {
			return total, nil
		}
		if !m.Monitored {
			continue
		}
		f, err := s.store.MediaFileForMovie(ctx, m.ID)
		if err != nil {
			return total, err
		}
		if f != nil {
			continue
		}
		processed++
		n, err := s.SearchMovie(ctx, m.ID)
		if err != nil {
			slog.Warn("automation: sweep movie search failed", "movieId", m.ID, "err", err)
			continue
		}
		total += n
	}

	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return total, err
	}
	for _, se := range series {
		if processed >= batch {
			return total, nil
		}
		if !se.Monitored {
			continue
		}
		processed++
		n, err := s.SearchSeries(ctx, se.ID)
		if err != nil {
			slog.Warn("automation: sweep series search failed", "seriesId", se.ID, "err", err)
			continue
		}
		total += n
	}
	return total, nil
}
