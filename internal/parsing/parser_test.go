package parsing

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
)

func TestParseQualityAttributes(t *testing.T) {
	cases := []struct {
		title string
		src   Source
		res   Resolution
		codec string
		rev   Revision
	}{
		{"The.Show.S01E01.1080p.BluRay.x264-GRP", SourceBluray, Res1080p, "x264", Revision{Version: 1}},
		{"Movie.Title.2019.2160p.WEB-DL.x265-GRP", SourceWEBDL, Res2160p, "x265", Revision{Version: 1}},
		{"Some.Show.S02E03.720p.HDTV.x264-GRP", SourceHDTV, Res720p, "x264", Revision{Version: 1}},
		{"Film.1998.480p.DVDRip.XviD-GRP", SourceDVD, Res480p, "xvid", Revision{Version: 1}},
		{"Show.S01E05.1080p.WEBRip.PROPER.x264-GRP", SourceWEBRip, Res1080p, "x264", Revision{Version: 2, IsRepack: false}},
		{"Show.S01E06.1080p.BluRay.REPACK.x265-GRP", SourceBluray, Res1080p, "x265", Revision{Version: 2, IsRepack: true}},
	}
	for _, c := range cases {
		got := Parse(c.title, provider.KindTV)
		if got.Source != c.src || got.Resolution != c.res || got.Codec != c.codec || got.Revision != c.rev {
			t.Errorf("Parse(%q) = {src:%v res:%v codec:%q rev:%+v}, want {src:%v res:%v codec:%q rev:%+v}",
				c.title, got.Source, got.Resolution, got.Codec, got.Revision, c.src, c.res, c.codec, c.rev)
		}
	}
}

func TestParseUnknownIsBestEffort(t *testing.T) {
	got := Parse("just some random text", provider.KindMovie)
	if got.Source != SourceUnknown || got.Resolution != ResUnknown {
		t.Fatalf("expected unknown src/res, got src:%v res:%v", got.Source, got.Resolution)
	}
}

func TestParseIdentityTV(t *testing.T) {
	cases := []struct {
		title    string
		wantS    int
		wantEps  []int
		wantName string
	}{
		{"The.Show.S02E05.1080p.BluRay.x264-GRP", 2, []int{5}, "The Show"},
		{"The.Show.S01E01E02.720p.HDTV.x264-GRP", 1, []int{1, 2}, "The Show"},
		{"The.Show.S03E10-E12.1080p.WEB-DL-GRP", 3, []int{10, 11, 12}, "The Show"},
	}
	for _, c := range cases {
		got := Parse(c.title, provider.KindTV)
		if got.Season != c.wantS || !equalInts(got.Episodes, c.wantEps) || got.Title != c.wantName {
			t.Errorf("Parse(%q) season=%d eps=%v title=%q; want season=%d eps=%v title=%q",
				c.title, got.Season, got.Episodes, got.Title, c.wantS, c.wantEps, c.wantName)
		}
	}
}

func TestParseIdentityMovie(t *testing.T) {
	got := Parse("Movie.Title.2019.Extended.1080p.BluRay.x264-GRP", provider.KindMovie)
	if got.Year != 2019 || got.Title != "Movie Title" || got.Edition != "Extended" {
		t.Fatalf("year=%d title=%q edition=%q", got.Year, got.Title, got.Edition)
	}
}

func TestParseGroupAndLanguage(t *testing.T) {
	got := Parse("Movie.Title.2019.MULTi.1080p.BluRay.x264-SOMEGRP", provider.KindMovie)
	if got.ReleaseGroup != "SOMEGRP" {
		t.Errorf("group=%q want SOMEGRP", got.ReleaseGroup)
	}
	if len(got.Languages) == 0 || got.Languages[0] != "multi" {
		t.Errorf("languages=%v want [multi]", got.Languages)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
