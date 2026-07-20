package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
	"github.com/hellboundg/nexus/internal/quality"
)

// UpgradeCompleted is emitted when an upgrade / cutoff-unmet sweep finishes.
type UpgradeCompleted struct {
	Grabbed int `json:"grabbed"`
}

func (UpgradeCompleted) Name() string { return "automation.upgrade.completed" }

func movieKey(id int64) string  { return fmt.Sprintf("movie:%d", id) }
func seriesKey(id int64) string { return fmt.Sprintf("series:%d", id) }

type cooldownKey struct {
	item  string
	title string
}

// cooldownSet is the set of (itemKey, normalized release title) pairs grabbed
// within the cooldown window. A candidate matching one must not be re-grabbed —
// this closes the mislabel re-grab loop (a release whose title claims a quality
// its file does not deliver would otherwise be grabbed every sweep).
type cooldownSet map[cooldownKey]struct{}

// buildCooldownSet keys grabbed-history events by their item and normalized
// source title. TV grabbed rows carry series_id (not episode_id), so TV keys are
// series-level; events with neither a movie nor a series id are ignored.
func buildCooldownSet(events []store.HistoryEvent) cooldownSet {
	cs := make(cooldownSet, len(events))
	for _, e := range events {
		var item string
		switch {
		case e.MovieID != nil:
			item = movieKey(*e.MovieID)
		case e.SeriesID != nil:
			item = seriesKey(*e.SeriesID)
		default:
			continue
		}
		cs[cooldownKey{item: item, title: normTitle(e.SourceTitle)}] = struct{}{}
	}
	return cs
}

func (cs cooldownSet) has(itemKey, title string) bool {
	_, ok := cs[cooldownKey{item: itemKey, title: normTitle(title)}]
	return ok
}

// upgradeCandidates keeps only candidates that are a genuine upgrade over the
// existing file's quality AND were not grabbed for this item within the cooldown
// window. Input is assumed already ranked best-first by Decide; order is
// preserved.
func upgradeCandidates(cands []Candidate, existingQualityID int, profile store.QualityProfile, itemKey string, cs cooldownSet) []Candidate {
	var out []Candidate
	for _, c := range cands {
		newID := quality.Resolve(c.Parsed).ID
		if !quality.IsUpgrade(newID, existingQualityID, profile) {
			continue
		}
		if cs.has(itemKey, c.Release.Title) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// UpgradeSweep processes up to batch monitored targets that already have a file
// ranking below their profile cutoff: monitored movies first, then each
// monitored episode of each monitored series. For each it searches, keeps only
// releases that are a genuine upgrade over the existing file and are not on
// cooldown, and grabs the best. Returns the total grabbed; a per-target error is
// logged and the sweep continues.
func (s *Service) UpgradeSweep(ctx context.Context, batch int) (int, error) {
	n, err := s.upgradeSweep(ctx, batch)
	s.emit(ctx, UpgradeCompleted{Grabbed: n})
	return n, err
}

func (s *Service) upgradeSweep(ctx context.Context, batch int) (int, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	if batch <= 0 {
		batch = cfg.UpgradeSearchBatchSize
	}
	since := time.Now().Add(-time.Duration(cfg.UpgradeGrabCooldownHours) * time.Hour)
	events, err := s.store.GrabbedSince(ctx, since)
	if err != nil {
		return 0, err
	}
	cs := buildCooldownSet(events)

	activeMovies, activeEps, inFlight, err := s.activeQueue(ctx)
	if err != nil {
		return 0, err
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
		if f == nil {
			continue // no file → that is the missing sweep's job
		}
		profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
		if err != nil {
			return total, err
		}
		if !ok || !quality.CutoffUnmet(f.QualityID, profile) {
			continue
		}
		if _, queued := activeMovies[m.ID]; queued {
			continue
		}
		processed++
		grabbed, err := s.upgradeMovie(ctx, m, f, profile, cs)
		if err != nil {
			slog.Warn("automation: upgrade movie search failed", "movieId", m.ID, "err", err)
			continue
		}
		total += grabbed
	}

	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return total, err
	}
	for _, se := range series {
		if processed >= batch {
			return total, nil
		}
		// Episode-level monitoring governs (see searchSeries), but skip a series
		// with nothing monitored rather than load its episode list to upgrade zero.
		hasMon, err := s.store.HasMonitoredEpisodes(ctx, se.ID)
		if err != nil {
			return total, err
		}
		if !hasMon {
			continue
		}
		profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
		if err != nil {
			return total, err
		}
		if !ok {
			continue
		}
		eps, err := s.store.ListEpisodes(ctx, se.ID)
		if err != nil {
			return total, err
		}
		bud := newBudget(cfg.MaxConcurrentPerSeries, inFlight[se.ID])
		for _, e := range eps {
			if !bud.allows() {
				break
			}
			if processed >= batch {
				return total, nil
			}
			if !e.Monitored {
				continue
			}
			f, err := s.store.MediaFileForEpisode(ctx, e.ID)
			if err != nil {
				return total, err
			}
			if f == nil || !quality.CutoffUnmet(f.QualityID, profile) {
				continue
			}
			if _, queued := activeEps[e.ID]; queued {
				continue
			}
			processed++
			grabbed, err := s.upgradeEpisode(ctx, &se, e, f, profile, cs)
			if err != nil {
				slog.Warn("automation: upgrade episode search failed", "episodeId", e.ID, "err", err)
				continue
			}
			if grabbed > 0 {
				bud.take()
			}
			total += grabbed
		}
	}
	return total, nil
}

func (s *Service) upgradeMovie(ctx context.Context, m store.Movie, f *store.MediaFile, profile store.QualityProfile, cs cooldownSet) (int, error) {
	releases, err := s.search.Search(ctx, movieQuery(&m))
	if err != nil {
		slog.Warn("automation: upgrade movie search had indexer errors", "movieId", m.ID, "err", err)
	}
	cands := upgradeCandidates(Decide(releases, provider.KindMovie, profile), f.QualityID, profile, movieKey(m.ID), cs)
	blocked, err := s.store.BlocklistedTitles(ctx, &m.ID, nil)
	if err != nil {
		slog.Warn("automation: blocklist lookup failed", "movieId", m.ID, "err", err)
	}
	_, grabbed, err := s.enqueueBest(ctx, cands, blocked, func(c Candidate) importing.EnqueueRequest {
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

func (s *Service) upgradeEpisode(ctx context.Context, se *store.Series, e store.Episode, f *store.MediaFile, profile store.QualityProfile, cs cooldownSet) (int, error) {
	ep := e.EpisodeNumber
	releases, err := s.search.Search(ctx, tvQuery(se, e.SeasonNumber, &ep))
	if err != nil {
		slog.Warn("automation: upgrade episode search had indexer errors", "episodeId", e.ID, "err", err)
	}
	var covering []Candidate
	for _, c := range Decide(releases, provider.KindTV, profile) {
		// Same loose-`q` hazard as the search path: an upgrade must not swap in
		// a better-scoring episode of a different show (see releaseIsForSeries).
		if !releaseIsForSeries(se, c.Parsed) {
			continue
		}
		if c.Parsed.Season == e.SeasonNumber && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
			covering = append(covering, c)
		}
	}
	covering = upgradeCandidates(covering, f.QualityID, profile, seriesKey(se.ID), cs)
	blocked, err := s.store.BlocklistedTitles(ctx, nil, &se.ID)
	if err != nil {
		slog.Warn("automation: blocklist lookup failed", "seriesId", se.ID, "err", err)
	}
	_, grabbed, err := s.enqueueBest(ctx, covering, blocked, func(c Candidate) importing.EnqueueRequest {
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
