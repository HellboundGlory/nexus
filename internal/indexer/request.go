package indexer

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/hellboundg/nexus/internal/core/provider"
)

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
		v.Set("q", q.Term)
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
