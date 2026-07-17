package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/quality"
)

// profile480Only allows 480p (SDTV) and explicitly disallows 1080p WEB-DL while
// keeping it PRESENT in the profile. This is the §5.3 trap: profileRank returns
// -1 only for qualities ABSENT from the profile — a present-but-not-allowed
// quality returns its REAL index, so quality.Compare ranks the rejected 1080p
// ABOVE the accepted 480p. Accepted-first must therefore be an explicit sort key.
func profile480Only() store.QualityProfile {
	return store.QualityProfile{
		Name: "480p only",
		Items: []store.QualityProfileItem{
			{QualityID: qualityIDFor("SDTV"), Allowed: true},
			{QualityID: qualityIDFor("WEBDL-1080p"), Allowed: false},
		},
		CutoffQualityID: qualityIDFor("SDTV"),
	}
}

func TestDecideAllSortsAcceptedFirstDespiteHigherProfileRank(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Movie.2019.1080p.WEB-DL.x264-GRP", IndexerID: "1"},
		{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindMovie, profile480Only(), nil, nil)

	if len(got) != 2 {
		t.Fatalf("want 2 scored candidates (rejects are kept, not dropped), got %d", len(got))
	}
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the ACCEPTED 480p release, got %q with rejections %v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "quality not in profile" {
		t.Fatalf("row 2 rejections = %v, want the not-allowed 1080p labelled", got[1].Rejections)
	}
}

func TestDecideAllLabelsBlocklistedWithReason(t *testing.T) {
	title := "Some.Movie.2019.480p.HDTV.x264-GRP"
	releases := []provider.Release{{Title: title, IndexerID: "1"}}
	blocked := map[string]string{store.NormReleaseTitle(title): "Not on your server(s)"}

	got := DecideAll(releases, provider.KindMovie, profile480Only(), blocked, nil)
	if len(got) != 1 {
		t.Fatalf("want the blocklisted release KEPT and labelled, got %d rows", len(got))
	}
	if got[0].Rejections[0] != "blocklisted: Not on your server(s)" {
		t.Fatalf("rejections = %v, want the blocklist reason surfaced", got[0].Rejections)
	}
}

func TestDecideAllLabelsNonCoveringEpisode(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Show.S01E05.480p.HDTV.x264-GRP", IndexerID: "1"},
		{Title: "Some.Show.S01E09.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindTV, profile480Only(), nil, EpisodeCoverage(1, 5))
	if len(got) != 2 {
		t.Fatalf("want both releases kept, got %d", len(got))
	}
	if len(got[0].Rejections) != 0 || got[0].Parsed.Episodes[0] != 5 {
		t.Fatalf("row 1 must be the covering S01E05, got %q rejections=%v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "does not cover S01E05" {
		t.Fatalf("row 2 rejections = %v, want the non-covering release labelled", got[1].Rejections)
	}
}

func TestDecideAllSeasonPackCoverageRejectsSingleEpisode(t *testing.T) {
	releases := []provider.Release{
		{Title: "Some.Show.S01.480p.HDTV.x264-GRP", IndexerID: "1"},
		{Title: "Some.Show.S01E03.480p.HDTV.x264-GRP", IndexerID: "1"},
	}
	got := DecideAll(releases, provider.KindTV, profile480Only(), nil, SeasonPackCoverage(1))
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 must be the season pack, got %q rejections=%v",
			got[0].Release.Title, got[0].Rejections)
	}
	if got[1].Rejections[0] != "not a season 1 pack" {
		t.Fatalf("row 2 rejections = %v, want the single episode labelled", got[1].Rejections)
	}
}

// The §5.3 property, tested against ALL THREE filters that define it. This is
// what makes "row 1 == what auto-search would have grabbed" real rather than
// aspirational: a candidate is clean only if it passes quality AND blocklist AND
// coverage — the same three filters Decide + enqueueBest + the search strategy
// apply on the automatic path.
func TestDecideAllRowOneMatchesWhatAutomationWouldGrab(t *testing.T) {
	blockedTitle := "Some.Show.S01E05.480p.HDTV.x264-BLOCKED"
	releases := []provider.Release{
		{Title: "Some.Show.S01E05.1080p.WEB-DL.x264-GRP", IndexerID: "1"},  // quality-rejected
		{Title: blockedTitle, IndexerID: "1"},                              // blocklisted
		{Title: "Some.Show.S01E09.480p.HDTV.x264-GRP", IndexerID: "1"},     // non-covering
		{Title: "Some.Show.S01E05.480p.HDTV.x264-GOOD", IndexerID: "1"},    // clean
	}
	blocked := map[string]string{store.NormReleaseTitle(blockedTitle): "Not on your server(s)"}

	got := DecideAll(releases, provider.KindTV, profile480Only(), blocked, EpisodeCoverage(1, 5))
	if len(got) != 4 {
		t.Fatalf("want all 4 releases kept, got %d", len(got))
	}
	if got[0].Release.Title != "Some.Show.S01E05.480p.HDTV.x264-GOOD" {
		t.Fatalf("row 1 = %q, want the only release passing all three filters", got[0].Release.Title)
	}
	if len(got[0].Rejections) != 0 {
		t.Fatalf("row 1 rejections = %v, want empty (== automation would grab it)", got[0].Rejections)
	}
	for _, c := range got[1:] {
		if len(c.Rejections) == 0 {
			t.Fatalf("row %q has no rejections but must have one", c.Release.Title)
		}
	}
}

func TestDecideAllRejectionsAlwaysNonNil(t *testing.T) {
	releases := []provider.Release{{Title: "Some.Movie.2019.480p.HDTV.x264-GRP", IndexerID: "1"}}
	got := DecideAll(releases, provider.KindMovie, profile480Only(), nil, nil)
	if got[0].Rejections == nil {
		t.Fatal("Rejections must be a non-nil empty slice so it serialises as [] not null")
	}
}

// qualityIDFor resolves a built-in quality definition id by name via the public
// quality API, so the tests do not hardcode ladder indices.
func qualityIDFor(name string) int {
	for _, d := range quality.Definitions() {
		if d.Name == name {
			return d.ID
		}
	}
	panic("unknown quality name in test: " + name)
}
