package parsing

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseSeasonPack(t *testing.T) {
	cases := []struct {
		title  string
		season int
	}{
		{"The.Show.S01.1080p.BluRay.x264-GRP", 1},
		{"The.Show.Season.2.1080p.WEB-DL-GRP", 2},
		{"The.Show.S03.COMPLETE.1080p.BluRay-GRP", 3},
	}
	for _, c := range cases {
		p := Parse(c.title, provider.KindTV)
		if p.Season != c.season || len(p.Episodes) != 0 {
			t.Fatalf("%q: want season=%d episodes=[], got season=%d episodes=%v",
				c.title, c.season, p.Season, p.Episodes)
		}
	}
}

func TestParseSeasonPackDoesNotBreakEpisodes(t *testing.T) {
	p := Parse("The.Show.S01E01.1080p.BluRay.x264-GRP", provider.KindTV)
	if p.Season != 1 || len(p.Episodes) != 1 || p.Episodes[0] != 1 {
		t.Fatalf("SxxExx must be unchanged, got season=%d episodes=%v", p.Season, p.Episodes)
	}
}

func TestParseNoSeasonStaysZero(t *testing.T) {
	p := Parse("Some.Movie.2020.1080p.BluRay.x264-GRP", provider.KindTV)
	if p.Season != 0 || len(p.Episodes) != 0 {
		t.Fatalf("non-season title should stay season=0, got season=%d episodes=%v", p.Season, p.Episodes)
	}
}
