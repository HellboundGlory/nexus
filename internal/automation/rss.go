package automation

import (
	"regexp"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

var (
	reNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	reTitleYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
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
	// p.Title from parsing.Parse(_, KindTV) can still carry a bare year token
	// (cleanTitle only cuts at resolution/source/season-episode markers for TV),
	// e.g. "Clone.2015.S01E01..." -> p.Title == "Clone 2015". Strip it before the
	// title lookup so it matches the index key built from the stored series title.
	cands := idx.seriesByTitle[normTitle(reTitleYear.ReplaceAllString(p.Title, " "))]
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
