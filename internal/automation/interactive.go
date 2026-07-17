package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// ScoredCandidate is a release annotated with why automation would or would not
// have grabbed it. Unlike Candidate (which only ever holds releases that passed
// the accept gate), a ScoredCandidate is produced for EVERY release the indexers
// returned — the whole point of interactive search is to show the rejects.
//
// Rejections is the uniform reason model: an EMPTY Rejections means automation
// would have grabbed this release. That gives the UI one rule (any reasons →
// grey the row + confirm before grabbing) and mirrors Sonarr's rejections array.
type ScoredCandidate struct {
	Candidate
	Decision   quality.Decision
	Rejections []string
}

// Coverage reports why a release does not cover the search target, or "" if it
// does. It is the interactive-search mirror of the per-strategy filters in
// search.go (searchSeason's pack filter, searchEpisode's covering filter) — but
// it annotates instead of dropping. A nil Coverage means no coverage constraint,
// which is the movie case.
type Coverage func(p parsing.ParsedRelease) string

// SeasonPackCoverage accepts only a full pack for the given season: the same
// predicate searchSeason applies at search.go:245 (right season, no episode
// numbers == a pack rather than a single episode).
func SeasonPackCoverage(season int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && len(p.Episodes) == 0 {
			return ""
		}
		return fmt.Sprintf("not a season %d pack", season)
	}
}

// EpisodeCoverage accepts only releases containing the given episode: the same
// predicate searchEpisode applies at search.go:326.
func EpisodeCoverage(season, episode int) Coverage {
	return func(p parsing.ParsedRelease) string {
		if p.Season == season && containsInt(p.Episodes, episode) {
			return ""
		}
		return fmt.Sprintf("does not cover S%02dE%02d", season, episode)
	}
}

// DecideAll is Decide's interactive sibling: it parses and ranks every release
// exactly as Decide does, but ANNOTATES the ones automation would discard rather
// than dropping them. Decide itself is deliberately untouched so the automatic
// paths cannot regress.
//
// It reproduces all three of automation's filters as annotations — quality
// (Decide's gate), blocklist (enqueueBest's filterBlocklisted), and coverage
// (the search strategy's own filter) — because the guarantee this function sells
// is that row 1 is exactly what auto-search would have grabbed. Annotating only
// some of the three would float a release automation would never take to the top.
//
// blocked maps normalised release title -> blocklist reason (store.BlocklistedReasons);
// nil means nothing is blocked. cover may be nil (no coverage constraint).
func DecideAll(releases []provider.Release, kind provider.MediaKind, profile store.QualityProfile, blocked map[string]string, cover Coverage) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(releases))
	for _, r := range releases {
		p := parsing.Parse(r.Title, kind)
		decision := quality.Decide(p, profile)

		// Non-nil so the DTO serialises [] rather than null (design §5.5).
		rejections := []string{}
		if !decision.Accepted {
			reason := decision.RejectionReason
			if reason == "" {
				reason = "quality not in profile"
			}
			rejections = append(rejections, reason)
		}
		if reason, ok := blocked[store.NormReleaseTitle(r.Title)]; ok {
			rejections = append(rejections, "blocklisted: "+reason)
		}
		if cover != nil {
			if reason := cover(p); reason != "" {
				rejections = append(rejections, reason)
			}
		}

		out = append(out, ScoredCandidate{
			Candidate:  Candidate{Release: r, Parsed: p},
			Decision:   decision,
			Rejections: rejections,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := len(out[i].Rejections) == 0, len(out[j].Rejections) == 0
		// Accepted-first MUST be an explicit key. It is tempting to assume the
		// rejects sink for free because profileRank returns -1 for qualities
		// absent from the profile — but a quality PRESENT and not allowed returns
		// its REAL index (decision.go:47-54), so under [480p allowed, 1080p
		// not-allowed] quality.Compare ranks the rejected 1080p ABOVE the
		// accepted 480p. Without this key, row 1 would be a release automation
		// would never grab.
		if ci != cj {
			return ci
		}
		return compare(out[i].Candidate, out[j].Candidate, profile) > 0
	})
	return out
}

// ErrNoProfile is returned by the interactive entry points when the target item
// has no quality profile assigned. DecideAll needs a profile to score against,
// and importing.Enqueue would reject the grab anyway, so a profile-less item
// could otherwise open a modal it could never grab from.
var ErrNoProfile = errors.New("automation: item has no quality profile")

// ScoredRelease is one row of the interactive list.
//
// provider.Release carries NO json tags, so this DTO spells every field out
// rather than embedding it — the wire shape must not depend on Go field names.
type ScoredRelease struct {
	Title       string            `json:"title"`
	DownloadURL string            `json:"downloadUrl"`
	InfoURL     string            `json:"infoUrl,omitempty"`
	Size        int64             `json:"size"`
	IndexerID   string            `json:"indexerId"`
	Protocol    provider.Protocol `json:"protocol"`
	PublishDate time.Time         `json:"publishDate"`
	// Seeders is a POINTER + omitempty: ABSENT on usenet rows, PRESENT on
	// torrents including a real 0. Never use the numeric value as a
	// presence discriminator (the C2 wire-shape trap).
	Seeders *int `json:"seeders,omitempty"`
	// Quality is always present — "Unknown" (ID 0) for an unparseable title,
	// because quality.Resolve never fails.
	Quality  quality.QualityDefinition `json:"quality"`
	Score    int                       `json:"score"`
	Accepted bool                      `json:"accepted"`
	// Rejections is always a non-nil array. Empty means automation would have
	// grabbed this release.
	Rejections []string `json:"rejections"`
}

// InteractiveResult mirrors indexer.SearchResult's shape. Both arrays are
// non-nil on the wire.
type InteractiveResult struct {
	Releases      []ScoredRelease `json:"releases"`
	IndexerErrors []IndexerError  `json:"indexerErrors"`
}

func toScoredReleases(cands []ScoredCandidate) []ScoredRelease {
	out := make([]ScoredRelease, 0, len(cands))
	for _, c := range cands {
		out = append(out, ScoredRelease{
			Title:       c.Release.Title,
			DownloadURL: c.Release.DownloadURL,
			InfoURL:     c.Release.InfoURL,
			Size:        c.Release.Size,
			IndexerID:   c.Release.IndexerID,
			Protocol:    c.Release.Protocol,
			PublishDate: c.Release.PublishDate,
			Seeders:     c.Release.Seeders,
			Quality:     c.Decision.Quality,
			Score:       c.Decision.Score,
			Accepted:    c.Decision.Accepted,
			Rejections:  c.Rejections,
		})
	}
	return out
}

func result(cands []ScoredCandidate, errs []IndexerError) InteractiveResult {
	if errs == nil {
		errs = []IndexerError{}
	}
	return InteractiveResult{Releases: toScoredReleases(cands), IndexerErrors: errs}
}

// InteractiveSearchMovie returns every release the indexers hold for a movie,
// each annotated with why automation would or would not grab it. Unlike
// SearchMovie it grabs nothing, and it deliberately does NOT skip unmonitored or
// already-filed items — the user asked for this list explicitly.
func (s *Service) InteractiveSearchMovie(ctx context.Context, movieID int64) (InteractiveResult, error) {
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	releases, idxErrs := s.search.SearchDetailed(ctx, movieQuery(m))
	blocked, err := s.store.BlocklistedReasons(ctx, &m.ID, nil)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "movieId", m.ID, "err", err)
	}
	return result(DecideAll(releases, provider.KindMovie, profile, blocked, nil), idxErrs), nil
}

// InteractiveSearchSeason lists releases for a whole season. Coverage is the
// season-pack predicate — the same one searchSeason applies — so a single
// episode is shown but labelled rather than silently dropped.
func (s *Service) InteractiveSearchSeason(ctx context.Context, seriesID int64, seasonNumber int) (InteractiveResult, error) {
	se, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	releases, idxErrs := s.search.SearchDetailed(ctx, tvQuery(se, seasonNumber, nil))
	blocked, err := s.store.BlocklistedReasons(ctx, nil, &se.ID)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "seriesId", se.ID, "err", err)
	}
	cands := DecideAll(releases, provider.KindTV, profile, blocked, SeasonPackCoverage(seasonNumber))
	return result(cands, idxErrs), nil
}

// InteractiveSearchEpisode lists releases for one episode.
func (s *Service) InteractiveSearchEpisode(ctx context.Context, episodeID int64) (InteractiveResult, error) {
	e, err := s.store.GetEpisode(ctx, episodeID)
	if err != nil {
		return InteractiveResult{}, err
	}
	se, err := s.store.GetSeries(ctx, e.SeriesID)
	if err != nil {
		return InteractiveResult{}, err
	}
	profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
	if err != nil {
		return InteractiveResult{}, err
	}
	if !ok {
		return InteractiveResult{}, ErrNoProfile
	}
	ep := e.EpisodeNumber
	releases, idxErrs := s.search.SearchDetailed(ctx, tvQuery(se, e.SeasonNumber, &ep))
	blocked, err := s.store.BlocklistedReasons(ctx, nil, &e.SeriesID)
	if err != nil {
		slog.Warn("automation: interactive blocklist lookup failed", "episodeId", e.ID, "err", err)
	}
	cands := DecideAll(releases, provider.KindTV, profile, blocked, EpisodeCoverage(e.SeasonNumber, e.EpisodeNumber))
	return result(cands, idxErrs), nil
}
