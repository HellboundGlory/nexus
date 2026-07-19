package indexer

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// titleSpecials removes the punctuation that metadata titles carry but ASCII
// scene release names never do, mirroring the arr apps' SearchCriteriaBase
// SpecialCharacter set (['.`´‘’]). Each is deleted, NOT replaced with a space,
// so "Marvel's Daredevil" collapses to "MarvelsDaredevil"-style tokens exactly
// as scene names spell them ("Marvels.Daredevil...").
var titleSpecials = strings.NewReplacer(
	"'", "", // U+0027 apostrophe
	"’", "", // ’ right single quotation mark (TMDb frequently uses this)
	"‘", "", // ‘ left single quotation mark
	"`", "", // ` grave accent
	"´", "", // ´ acute accent
	".", "", // period — mirrors the arr set (S.W.A.T. → SWAT)
)

// cleanTerm normalizes a search term for Newznab/Torznab q matching, mirroring
// the arr apps' GetCleanSceneTitle: it first deletes apostrophes and related
// punctuation (titleSpecials), then folds diacritics (é→e). Newznab indexers
// match q LITERALLY against ASCII scene names, so both an apostrophe
// ("Marvel's Daredevil") and an accent ("Pokémon") otherwise return nothing —
// verified against NZBGeek: q with the apostrophe returned 0 items where the
// stripped spelling returned 43. Accent folding is decomposable marks only —
// non-decomposable letters (ø, ß, œ) and non-Latin scripts pass through
// unchanged rather than being mangled.
func cleanTerm(s string) string {
	s = titleSpecials.Replace(s)
	out, _, err := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		return s
	}
	return out
}

// buildSearchURL constructs a Newznab/Torznab API request URL.
func buildSearchURL(base, apiKey string, q provider.Query) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/api")
	if err != nil {
		return "", err
	}
	t := q.Type
	if t == "" {
		t = provider.SearchGeneric
	}
	v := url.Values{}
	v.Set("t", string(t))
	if apiKey != "" {
		v.Set("apikey", apiKey)
	}
	if q.Term != "" {
		v.Set("q", cleanTerm(q.Term))
	}
	if len(q.Categories) > 0 {
		cats := make([]string, len(q.Categories))
		for i, c := range q.Categories {
			cats[i] = strconv.Itoa(c)
		}
		v.Set("cat", strings.Join(cats, ","))
	}
	if q.Season != nil {
		v.Set("season", strconv.Itoa(*q.Season))
	}
	if q.Episode != nil {
		v.Set("ep", strconv.Itoa(*q.Episode))
	}
	if q.IMDbID != "" {
		v.Set("imdbid", strings.TrimPrefix(q.IMDbID, "tt"))
	}
	if q.TVDBID != 0 {
		v.Set("tvdbid", strconv.Itoa(q.TVDBID))
	}
	if q.TMDBID != 0 {
		v.Set("tmdbid", strconv.Itoa(q.TMDBID))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	u.RawQuery = v.Encode()
	return u.String(), nil
}
