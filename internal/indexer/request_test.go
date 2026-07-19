package indexer

import (
	"net/url"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

// Scene release names are ASCII ("Pokemon.Detective.Pikachu.2019..."), but
// metadata titles carry diacritics ("Pokémon Detective Pikachu"). Newznab
// indexers match q literally, so an accented q returns nothing — verified
// against NZBGeek: q="Pokémon Detective Pikachu" returned 0 items where the
// folded spelling returned 74. The term must therefore be folded to ASCII.
func TestBuildSearchURLFoldsAccentsInTerm(t *testing.T) {
	raw, err := buildSearchURL("https://idx.test/", "KEY", provider.Query{
		Type: provider.SearchMovie, Term: "Pokémon Detective Pikachu", TMDBID: 447404,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.Query().Get("q"), "Pokemon Detective Pikachu"; got != want {
		t.Errorf("q = %q want %q", got, want)
	}
	// Folding must not disturb the id params.
	if got := u.Query().Get("tmdbid"); got != "447404" {
		t.Errorf("tmdbid = %q want %q", got, "447404")
	}
}

// "Marvel's Daredevil" (apostrophe) matched literally against ASCII scene names
// returns nothing — verified against NZBGeek: q with the apostrophe returned 0
// items where the stripped "Marvels Daredevil" returned 43. The apostrophe must
// be deleted (not spaced) so scene names like "Marvels.Daredevil.S01E01" match.
func TestBuildSearchURLStripsApostropheInTerm(t *testing.T) {
	season, ep := 1, 1
	raw, err := buildSearchURL("https://idx.test/", "KEY", provider.Query{
		Type: provider.SearchTV, Term: "Marvel's Daredevil", Season: &season, Episode: &ep,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.Query().Get("q"), "Marvels Daredevil"; got != want {
		t.Errorf("q = %q want %q", got, want)
	}
}

func TestCleanTerm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Pokémon Detective Pikachu", "Pokemon Detective Pikachu"},
		{"A Minecraft Movie", "A Minecraft Movie"}, // ASCII is unchanged
		{"", ""},
		{"Amélie", "Amelie"},
		{"Œuvre", "Œuvre"}, // non-decomposable: left alone, not mangled
		{"日本", "日本"},       // non-Latin passes through
		// Apostrophes are deleted, not spaced: scene names are "Marvels.Daredevil",
		// so q must be "Marvels Daredevil". Verified against NZBGeek: q with the
		// apostrophe returned 0 items where the stripped spelling returned 43.
		{"Marvel's Daredevil", "Marvels Daredevil"},        // ASCII apostrophe U+0027
		{"Marvel’s Daredevil", "Marvels Daredevil"},   // typographic ' U+2019 (TMDb often uses this)
		{"S.W.A.T.", "SWAT"},                               // periods deleted too (arr SpecialCharacter set)
	}
	for _, c := range cases {
		if got := cleanTerm(c.in); got != c.want {
			t.Errorf("cleanTerm(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

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
