package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func parsedTV(title string) parsing.ParsedRelease { return parsing.Parse(title, provider.KindTV) }

func TestReleaseIsForSeries(t *testing.T) {
	se := &store.Series{ID: 1, Title: "Pokémon"}
	// Series 1 owns two aliases; series 2 is a different show in the library.
	ti := titleIndex{
		"pokemon":               {1},
		"pokemon indigo league": {1},
		"pocket monsters":       {1},
		"pokemon horizons":      {2},
		"shared name":           {1, 2},
	}
	cases := []struct {
		name  string
		title string
		want  bool
	}{
		{"primary title", "Pokemon.S01E01.1080p.WEB-DL.x264-GRP", true},
		{"accented primary vs ascii release", "Pokemon.S01E01.720p.HDTV.x264-GRP", true},
		{"alias", "Pokemon.Indigo.League.s01e01", true},
		{"different show", "Pokemon.Trainer.Tour.S01E01.1080p.WEB-DL.x264-GRP", false},
		{"another library series", "Pokemon.Horizons.S01E01.1080p.WEB-DL.x264-GRP", false},
		{"ambiguous across two series", "Shared.Name.S01E01.1080p.WEB-DL.x264-GRP", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseIsForSeries(se, parsedTV(tc.title), ti); got != tc.want {
				t.Fatalf("releaseIsForSeries(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// A series whose aliases were never fetched must still match its own primary
// title, rather than failing open (accept everything) or closed (accept nothing).
func TestReleaseIsForSeriesFallsBackWithoutAliases(t *testing.T) {
	se := &store.Series{ID: 1, Title: "Pokémon"}
	empty := titleIndex{}
	if !releaseIsForSeries(se, parsedTV("Pokemon.S01E01.1080p.WEB-DL.x264-GRP"), empty) {
		t.Fatal("must fall back to primary-title matching when the index has no entry")
	}
	if releaseIsForSeries(se, parsedTV("Pokemon.Trainer.Tour.S01E01.1080p.WEB-DL.x264-GRP"), empty) {
		t.Fatal("fallback must still reject a different show")
	}
}
