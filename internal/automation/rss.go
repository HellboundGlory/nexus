package automation

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/importing"
	"github.com/hellboundg/nexus/internal/parsing"
)

var (
	reNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	reTitleYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reSeasonTok = regexp.MustCompile(`(?i)\bS\d{1,2}\b`)
)

// normTitle lowercases a title and collapses every run of non-alphanumeric
// characters to a single space, so "The.Matrix" and "The Matrix" compare equal.
func normTitle(s string) string {
	s = strings.ToLower(s)
	s = reNonAlnum.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// normIMDb strips the "tt" prefix so a release id (already tt-stripped by the
// parser) compares against a stored movie IMDbID regardless of its stored form.
func normIMDb(s string) string {
	return strings.TrimPrefix(strings.ToLower(s), "tt")
}

// titleYear returns the first 4-digit year in a raw title, or 0. Used to
// disambiguate same-titled TV series (parsing.Parse does not extract a year for
// the TV kind).
func titleYear(s string) int {
	if m := reTitleYear.FindString(s); m != "" {
		return atoiSafe(m)
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstAiredYear(se *store.Series) int {
	if len(se.FirstAired) >= 4 {
		return atoiSafe(se.FirstAired[:4])
	}
	return 0
}

// libraryIndex is an in-memory lookup over monitored library items, built once
// per RSS poll. Title maps hold slices because two monitored items can share a
// normalized title (disambiguated by year at match time).
type libraryIndex struct {
	movieByTMDB   map[int]*store.Movie
	movieByIMDb   map[string]*store.Movie
	movieByTitle  map[string][]*store.Movie
	seriesByTMDB  map[int]*store.Series
	seriesByTitle map[string][]*store.Series
}

func buildLibraryIndex(movies []store.Movie, series []store.Series) *libraryIndex {
	idx := &libraryIndex{
		movieByTMDB:   map[int]*store.Movie{},
		movieByIMDb:   map[string]*store.Movie{},
		movieByTitle:  map[string][]*store.Movie{},
		seriesByTMDB:  map[int]*store.Series{},
		seriesByTitle: map[string][]*store.Series{},
	}
	for i := range movies {
		m := &movies[i]
		if !m.Monitored {
			continue
		}
		if m.TMDBID != 0 {
			idx.movieByTMDB[m.TMDBID] = m
		}
		if m.IMDbID != "" {
			idx.movieByIMDb[normIMDb(m.IMDbID)] = m
		}
		key := normTitle(m.Title)
		idx.movieByTitle[key] = append(idx.movieByTitle[key], m)
	}
	for i := range series {
		se := &series[i]
		if !se.Monitored {
			continue
		}
		if se.TMDBID != 0 {
			idx.seriesByTMDB[se.TMDBID] = se
		}
		key := normTitle(se.Title)
		idx.seriesByTitle[key] = append(idx.seriesByTitle[key], se)
	}
	return idx
}

// matchMovie resolves a release to a monitored movie: tmdbid, then imdbid, then
// normalized title disambiguated by year. Returns false when nothing matches or
// the title is ambiguous.
func (idx *libraryIndex) matchMovie(r provider.Release, p parsing.ParsedRelease) (*store.Movie, bool) {
	if r.TMDBID != 0 {
		if m, ok := idx.movieByTMDB[r.TMDBID]; ok {
			return m, true
		}
	}
	if r.IMDbID != "" {
		if m, ok := idx.movieByIMDb[normIMDb(r.IMDbID)]; ok {
			return m, true
		}
	}
	cands := idx.movieByTitle[normTitle(p.Title)]
	var hits []*store.Movie
	for _, m := range cands {
		if p.Year == 0 || m.Year == 0 || m.Year == p.Year {
			hits = append(hits, m)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return nil, false
}

// matchSeries resolves a release to a monitored series: tmdbid, then normalized
// title disambiguated by the release year against the series' first-aired year.
func (idx *libraryIndex) matchSeries(r provider.Release, p parsing.ParsedRelease) (*store.Series, bool) {
	if r.TMDBID != 0 {
		if se, ok := idx.seriesByTMDB[r.TMDBID]; ok {
			return se, true
		}
	}
	// p.Title from parsing.Parse(_, KindTV) can still carry a bare year token or a
	// bare season token (cleanTitle only cuts at a full S##E## season+episode
	// marker for TV, so a season-pack-only title like "The.Show.S01..." has no
	// episode group to cut at), e.g. "Clone.2015.S01E01..." -> "Clone 2015", or
	// "The.Show.S01..." -> "The Show S01".
	//
	// The index key is built from the stored series title, un-stripped. Try the
	// un-stripped release title first so a series literally titled with a year
	// or season-like token (e.g. "1923") still matches its own un-stripped key,
	// and so a normal series isn't narrowed by a coincidental "year" in the
	// release title (e.g. "Foo.2019.S01E01" must not miss "Foo" via a stripped
	// key when nothing forces the strip). Only fall back to the year+season-
	// stripped key when the un-stripped lookup yields nothing.
	cands := idx.seriesByTitle[normTitle(p.Title)]
	if len(cands) == 0 {
		cleaned := reTitleYear.ReplaceAllString(p.Title, " ")
		cleaned = reSeasonTok.ReplaceAllString(cleaned, " ")
		cands = idx.seriesByTitle[normTitle(cleaned)]
	}
	if len(cands) == 1 {
		return cands[0], true
	}
	year := titleYear(r.Title)
	if year == 0 {
		return nil, false // ambiguous with no year to disambiguate
	}
	var hits []*store.Series
	for _, se := range cands {
		if firstAiredYear(se) == year {
			hits = append(hits, se)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return nil, false
}

// routeKind decides movie vs TV for a release. Newznab category wins
// (2000–2999 = movie, 5000–5999 = TV); otherwise a parse heuristic: a parsable
// season => TV, else a parsable year => movie. Returns false when undecidable.
func routeKind(r provider.Release) (provider.MediaKind, bool) {
	for _, c := range r.Categories {
		if c >= 2000 && c < 3000 {
			return provider.KindMovie, true
		}
		if c >= 5000 && c < 6000 {
			return provider.KindTV, true
		}
	}
	if parsing.Parse(r.Title, provider.KindTV).Season > 0 {
		return provider.KindTV, true
	}
	if parsing.Parse(r.Title, provider.KindMovie).Year > 0 {
		return provider.KindMovie, true
	}
	return "", false
}

// rssFeedLimit bounds each indexer's latest-feed response.
const rssFeedLimit = 100

// RSSResult summarizes one poll: releases seen, releases matched to a monitored
// item, and releases grabbed.
type RSSResult struct {
	Considered int
	Matched    int
	Grabbed    int
}

// RSSCompleted is emitted when an RSS poll finishes → WS.
type RSSCompleted struct {
	Considered int `json:"considered"`
	Matched    int `json:"matched"`
	Grabbed    int `json:"grabbed"`
}

func (RSSCompleted) Name() string { return "automation.rss.completed" }

// RSSSync polls every enabled indexer's latest feed once, reverse-matches each
// release to a monitored missing item, and grabs the best acceptable release per
// target. Release-driven, but grabs are decided per target (best-of-duplicates),
// reusing the same guards and Decide/enqueueBest as the wanted/missing search.
func (s *Service) RSSSync(ctx context.Context) (RSSResult, error) {
	releases, err := s.search.Search(ctx, provider.Query{Type: provider.SearchGeneric, Limit: rssFeedLimit})
	if err != nil {
		slog.Warn("automation: rss feed had indexer errors", "err", err)
	}
	res := RSSResult{Considered: len(releases)}

	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return res, err
	}
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return res, err
	}
	idx := buildLibraryIndex(movies, series)

	// Bucket releases by resolved target.
	movieRels := map[int64][]provider.Release{}
	movieTargets := map[int64]*store.Movie{}
	tvRels := map[int64][]provider.Release{}
	tvTargets := map[int64]*store.Series{}
	for _, r := range releases {
		kind, ok := routeKind(r)
		if !ok {
			continue
		}
		p := parsing.Parse(r.Title, kind)
		if kind == provider.KindMovie {
			m, ok := idx.matchMovie(r, p)
			if !ok {
				continue
			}
			movieRels[m.ID] = append(movieRels[m.ID], r)
			movieTargets[m.ID] = m
			res.Matched++
		} else {
			se, ok := idx.matchSeries(r, p)
			if !ok {
				continue
			}
			tvRels[se.ID] = append(tvRels[se.ID], r)
			tvTargets[se.ID] = se
			res.Matched++
		}
	}

	activeMovies, activeEps, err := s.activeQueue(ctx)
	if err != nil {
		return res, err
	}

	// Movies: skip filed/in-flight, then Decide + enqueue best of the bucket.
	for movieID, rels := range movieRels {
		if _, active := activeMovies[movieID]; active {
			continue
		}
		if f, err := s.store.MediaFileForMovie(ctx, movieID); err != nil {
			return res, err
		} else if f != nil {
			continue
		}
		m := movieTargets[movieID]
		profile, ok, err := s.profileFor(ctx, m.QualityProfileID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		cands := Decide(rels, provider.KindMovie, profile)
		_, grabbed, err := s.enqueueBest(ctx, cands, func(c Candidate) importing.EnqueueRequest {
			return importing.EnqueueRequest{
				DownloadURL: c.Release.DownloadURL, Title: c.Release.Title,
				Protocol: c.Release.Protocol, IndexerID: c.Release.IndexerID,
				MediaKind: provider.KindMovie, MovieID: movieID,
			}
		})
		if err != nil {
			return res, err
		}
		if grabbed {
			res.Grabbed++
		}
	}

	// TV: per series, rank the bucket once, then place season packs / episodes.
	for seriesID, rels := range tvRels {
		se := tvTargets[seriesID]
		profile, ok, err := s.profileFor(ctx, se.QualityProfileID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		eps, err := s.store.ListEpisodes(ctx, seriesID)
		if err != nil {
			return res, err
		}
		ranked := Decide(rels, provider.KindTV, profile)
		n, err := s.rssPlaceTV(ctx, se, eps, ranked, activeEps)
		if err != nil {
			return res, err
		}
		res.Grabbed += n
	}

	s.emit(ctx, RSSCompleted(res))
	return res, nil
}

// rssPlaceTV places ranked TV candidates against a series' monitored-missing
// episodes: a full-season pack first for any fully-missing monitored season
// (grabbed with all its missing episode ids), then per-episode for any still-
// unhandled missing episode. Mirrors the wanted/missing season strategy but over
// an already-fetched candidate pool.
func (s *Service) rssPlaceTV(ctx context.Context, se *store.Series, eps []store.Episode, ranked []Candidate, activeEps map[int64]struct{}) (int, error) {
	missingBySeason := map[int][]store.Episode{}
	monitoredBySeason := map[int]int{}
	for _, e := range eps {
		if !e.Monitored {
			continue
		}
		monitoredBySeason[e.SeasonNumber]++
		f, err := s.store.MediaFileForEpisode(ctx, e.ID)
		if err != nil {
			return 0, err
		}
		if _, active := activeEps[e.ID]; f == nil && !active {
			missingBySeason[e.SeasonNumber] = append(missingBySeason[e.SeasonNumber], e)
		}
	}

	handled := map[int64]struct{}{}
	grabbed := 0

	// Season packs first, for fully-missing monitored seasons.
	for season, missing := range missingBySeason {
		if len(missing) != monitoredBySeason[season] {
			continue
		}
		var packs []Candidate
		for _, c := range ranked {
			if c.Parsed.Season == season && len(c.Parsed.Episodes) == 0 {
				packs = append(packs, c)
			}
		}
		if len(packs) == 0 {
			continue
		}
		ids := episodeIDs(missing)
		_, ok, err := s.enqueueBest(ctx, packs, func(c Candidate) importing.EnqueueRequest {
			return tvRequest(se.ID, ids, c)
		})
		if err != nil {
			return grabbed, err
		}
		if ok {
			grabbed++
			for _, e := range missing {
				handled[e.ID] = struct{}{}
			}
		}
	}

	// Per-episode for anything not covered by a grabbed pack.
	for season, missing := range missingBySeason {
		for _, e := range missing {
			if _, done := handled[e.ID]; done {
				continue
			}
			var covering []Candidate
			for _, c := range ranked {
				if c.Parsed.Season == season && containsInt(c.Parsed.Episodes, e.EpisodeNumber) {
					covering = append(covering, c)
				}
			}
			if len(covering) == 0 {
				continue
			}
			// A covering candidate may be a multi-episode release (e.g.
			// S01E02E03) that also covers other still-missing episodes in this
			// season. Compute the full set of missing episodes the chosen
			// candidate covers so a single grab enqueues all of them together
			// and marks them all handled — otherwise the next missing episode
			// in this loop would find the same release still in `covering`
			// (activeEps is a poll-start snapshot, not updated mid-poll) and
			// grab it again, producing a duplicate download.
			chosen, ok, err := s.enqueueBest(ctx, covering, func(c Candidate) importing.EnqueueRequest {
				var ids []int64
				for _, m := range missing {
					if containsInt(c.Parsed.Episodes, m.EpisodeNumber) {
						ids = append(ids, m.ID)
					}
				}
				return tvRequest(se.ID, ids, c)
			})
			if err != nil {
				return grabbed, err
			}
			if ok {
				grabbed++
				for _, m := range missing {
					if containsInt(chosen.Parsed.Episodes, m.EpisodeNumber) {
						handled[m.ID] = struct{}{}
					}
				}
			}
		}
	}
	return grabbed, nil
}
