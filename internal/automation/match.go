package automation

import (
	"context"
	"strings"

	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

// titleIndex maps a normalized title to every series it could refer to. It is
// built from primary titles AND aliases, so a release named for an alternate
// scene title ("Pokemon.Indigo.League") still resolves to its series. A key with
// more than one series id is ambiguous.
type titleIndex map[string][]int64

// buildTitleIndex loads the whole library's titles in two queries. Callers build
// it once per search entry point and thread it down, exactly as they thread the
// per-series budget.
func (s *Service) buildTitleIndex(ctx context.Context) (titleIndex, error) {
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return nil, err
	}
	ti := titleIndex{}
	add := func(title string, id int64) {
		k := normTitle(title)
		if k == "" {
			return
		}
		for _, existing := range ti[k] {
			if existing == id {
				return
			}
		}
		ti[k] = append(ti[k], id)
	}
	for _, se := range series {
		add(se.Title, se.ID)
	}
	aliases, err := s.store.AllSeriesAliases(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		add(a.Title, a.SeriesID)
	}
	return ti, nil
}

// releaseIsForSeries reports whether a parsed release belongs to se.
//
// Search results cannot be trusted to be on-target: newznab matches its `q` term
// loosely, and Nexus cannot scope a TV search server-side (it sends a tmdbid, but
// newznab TV is keyed on tvdbid, which Nexus does not store — and a probe showed
// that even a tvdbid-scoped query still returns sibling shows). Without this
// check the best-SCORING release wins regardless of which show it belongs to.
//
// Matching is exact on the normalized title, never a prefix: a prefix test would
// re-accept "Pokemon Trainer Tour" for a series called "Pokemon".
func releaseIsForSeries(se *store.Series, p parsing.ParsedRelease, ti titleIndex) bool {
	key := normTitle(cleanedReleaseTitle(p.Title))
	if key == "" {
		return false
	}
	ids, known := ti[key]
	if !known {
		// No index entry: the library has never seen this title. Fall back to the
		// series' own primary title so a series whose aliases were never fetched
		// still matches its own releases.
		return key == normTitle(se.Title)
	}
	if len(ids) > 1 {
		return false // ambiguous across the library — refuse rather than guess
	}
	return ids[0] == se.ID
}

// cleanedReleaseTitle strips a bare season token ("S01") from a release's
// parsed title. It is load-bearing for season packs, not cosmetic:
// parsing.Parse's cleanTitle only cuts a TV title at the season+episode
// token (reSeasonEp), which requires an episode number. A season-pack-only
// release (no episode) has no episode token, so its parsed Title keeps the
// trailing "S01" -- e.g. "The Show S01" instead of "The Show" -- and would
// otherwise never match the series' own primary title or any alias.
//
// Deliberately does NOT also strip a year (reTitleYear), unlike the matcher
// this file replaces: a series titled with a bare year ("1923") would
// normalize to "" and become unmatchable against its own releases, and the
// titleIndex is keyed on the RAW (unstripped) series/alias title, so
// stripping the year here would desync the two sides for any year-suffixed
// title ("The Show 2020"). The cost is a real but narrow divergence: a
// release that happens to carry a year token the stored title lacks won't
// match via this path -- a safe missed grab, not a wrong one.
func cleanedReleaseTitle(title string) string {
	return reSeasonTok.ReplaceAllString(title, " ")
}

// episodeTitleContradicts reports whether a release's episode title positively
// contradicts the stored one.
//
// This is the only automatic signal that separates two shows sharing a scene
// name: "Pokemon.S01E01.The.Pendant.That.Starts.It.All" is Pokemon Horizons,
// while the monitored series' S01E01 is "Pokemon - I Choose You!". No
// series-title comparison can tell them apart.
//
// It vetoes ONLY on positive evidence. An absent or unrecognisable episode title
// on either side yields false, because a false rejection means nothing downloads
// while a missed signal merely falls through to the other checks.
func episodeTitleContradicts(stored string, p parsing.ParsedRelease) bool {
	got := normTitle(p.EpisodeTitle)
	want := normTitle(stored)
	if got == "" || want == "" {
		return false
	}
	// Either containing the other counts as agreement: release names abbreviate
	// ("I Choose You" vs "Pokemon - I Choose You!") and may carry a trailing
	// release-group suffix.
	if strings.Contains(want, got) || strings.Contains(got, want) {
		return false
	}
	return true
}
