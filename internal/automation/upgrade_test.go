package automation

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

func i64p(v int64) *int64 { return &v }

func TestUpgradeCompletedName(t *testing.T) {
	if (UpgradeCompleted{}).Name() != "automation.upgrade.completed" {
		t.Fatalf("bad event name %q", (UpgradeCompleted{}).Name())
	}
}

func TestBuildCooldownSetAndHas(t *testing.T) {
	events := []store.HistoryEvent{
		{EventType: "grabbed", MovieID: i64p(5), SourceTitle: "The.Film.2020.1080p.BluRay.x264-GRP"},
		{EventType: "grabbed", SeriesID: i64p(9), SourceTitle: "The.Show.S01E01.1080p.WEB-DL.x264-GRP"},
		{EventType: "grabbed", SourceTitle: "orphan-no-ids"}, // ignored: no movie/series id
	}
	cs := buildCooldownSet(events)
	if !cs.has(movieKey(5), "The.Film.2020.1080p.BluRay.x264-GRP") {
		t.Fatal("recent movie grab should be in cooldown set")
	}
	if !cs.has(seriesKey(9), "The.Show.S01E01.1080p.WEB-DL.x264-GRP") {
		t.Fatal("recent series grab should be in cooldown set")
	}
	if cs.has(movieKey(6), "The.Film.2020.1080p.BluRay.x264-GRP") {
		t.Fatal("different movie must not match")
	}
	if cs.has(movieKey(5), "Some.Other.Title") {
		t.Fatal("different title must not match")
	}
}

func TestUpgradeCandidatesFiltersNonUpgradesAndCooldown(t *testing.T) {
	p := hdProfile() // 7 & 9, cutoff 9
	mkCand := func(title string) Candidate {
		return Candidate{Release: provider.Release{Title: title}, Parsed: parsing.Parse(title, provider.KindMovie)}
	}
	web := mkCand("The.Film.2020.1080p.WEB-DL.x264-GRP") // quality 7
	blu := mkCand("The.Film.2020.1080p.BluRay.x264-GRP") // quality 9
	// Existing file is WEBDL-1080p(7); only the Bluray(9) is an upgrade.
	out := upgradeCandidates([]Candidate{web, blu}, 7, p, movieKey(1), cooldownSet{})
	if len(out) != 1 || out[0].Release.Title != blu.Release.Title {
		t.Fatalf("only the Bluray upgrade should survive, got %+v", out)
	}
	// Put the Bluray title on cooldown for this movie -> nothing survives.
	cs := buildCooldownSet([]store.HistoryEvent{
		{EventType: "grabbed", MovieID: i64p(1), SourceTitle: blu.Release.Title},
	})
	out = upgradeCandidates([]Candidate{web, blu}, 7, p, movieKey(1), cs)
	if len(out) != 0 {
		t.Fatalf("cooldown should suppress the only upgrade, got %+v", out)
	}
}
