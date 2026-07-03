package indexer

import (
	"net/url"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestBuildSearchURL(t *testing.T) {
	season, ep := 2, 5
	q := provider.Query{
		Type: provider.SearchTV, Term: "the show",
		Categories: []int{5000, 5040}, Season: &season, Episode: &ep,
		TVDBID: 12345, Limit: 100, Offset: 20,
	}
	raw, err := buildSearchURL("https://idx.test/", "KEY", q)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/api" {
		t.Fatalf("path = %q", u.Path)
	}
	vals := u.Query()
	checks := map[string]string{
		"t": "tvsearch", "apikey": "KEY", "q": "the show",
		"cat": "5000,5040", "season": "2", "ep": "5",
		"tvdbid": "12345", "limit": "100", "offset": "20",
	}
	for k, want := range checks {
		if got := vals.Get(k); got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
}

func TestBuildSearchURLDefaultsToGenericSearch(t *testing.T) {
	raw, err := buildSearchURL("https://idx.test", "K", provider.Query{Term: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("t") != "search" {
		t.Fatalf("t = %q want search", u.Query().Get("t"))
	}
}
