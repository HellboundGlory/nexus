package automation

import (
	"context"
	"fmt"
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

func fileMovie(t *testing.T, st *store.Store, qualityID int) int64 {
	t.Helper()
	id := seedMovie(t, st, true, true) // monitored, hdProfile (7/9, cutoff 9, upgrades on)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "movie", MovieID: &id, RelativePath: "m.mkv", QualityID: qualityID,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUpgradeSweepGrabsUpgrade(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 7) // existing WEBDL-1080p(7), below cutoff 9
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "blu" {
		t.Fatalf("below-cutoff movie should grab the Bluray upgrade: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestUpgradeSweepSkipsAtCutoffWithoutSearching(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 9) // already at cutoff
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.2160p.BluRay.x265-GRP", DownloadURL: "uhd", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("at-cutoff item must not be grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("at-cutoff item must not trigger an indexer search, got %+v", fs.lastQuery)
	}
}

func TestUpgradeSweepRejectsNonUpgrade(t *testing.T) {
	st := newStore(t)
	fileMovie(t, st, 7)
	// Only a same-quality WEBDL-1080p(7) is offered -> accepted by profile but not an upgrade.
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.WEB-DL.x264-OTHER", DownloadURL: "web", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("non-upgrade must not be grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type == "" {
		t.Fatalf("below-cutoff item should still have been searched")
	}
}

func TestUpgradeSweepSkipsRecentlyGrabbed(t *testing.T) {
	st := newStore(t)
	id := fileMovie(t, st, 7)
	title := "The.Film.2020.1080p.BluRay.x264-GRP"
	if err := st.AddHistory(context.Background(), store.HistoryEvent{
		EventType: "grabbed", MediaKind: "movie", MovieID: &id, SourceTitle: title,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: title, DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("release grabbed within cooldown must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestUpgradeSweepSkipsInFlight(t *testing.T) {
	st := newStore(t)
	id := fileMovie(t, st, 7)
	if _, err := st.EnqueueGrab(context.Background(), store.QueueItem{
		MediaKind: "movie", MovieID: &id, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("in-flight item must not be re-grabbed: n=%d reqs=%d", n, len(fe.reqs))
	}
	if fs.lastQuery.Type != "" {
		t.Fatalf("in-flight item must not trigger a search, got %+v", fs.lastQuery)
	}
}

func TestUpgradeSweepRespectsUpgradesDisabled(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 5000, IMDbID: "tt5000", Title: "The Film", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	p := hdProfile()
	p.Name = "NoUpgrade"
	p.UpgradeAllowed = false
	prof, err := st.CreateQualityProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieQualityProfileID(ctx, mid, &prof.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "m.mkv", QualityID: 7}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 || fs.lastQuery.Type != "" {
		t.Fatalf("upgrades-disabled profile must never search or grab: n=%d reqs=%d q=%+v", n, len(fe.reqs), fs.lastQuery)
	}
}

func TestUpgradeSweepTVEpisode(t *testing.T) {
	st := newStore(t)
	sid, epIDs := seedSeries(t, st, true, 1)
	_ = sid
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "tv", EpisodeID: &epIDs[0], RelativePath: "e1.mkv", QualityID: 7,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	n, err := svc.UpgradeSweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 || fe.reqs[0].EpisodeIDs[0] != epIDs[0] {
		t.Fatalf("below-cutoff episode should grab a covering upgrade: n=%d reqs=%+v", n, fe.reqs)
	}
	if fs.lastQuery.Episode == nil {
		t.Fatalf("TV upgrade search must be per-episode, got %+v", fs.lastQuery)
	}
}

// seedUpgradableSeries seeds a monitored series whose every episode already has
// a WEBDL-1080p(7) file — below hdProfile's cutoff of 9 — so every episode is a
// valid upgrade target. Mirrors TestUpgradeSweepTVEpisode (upgrade_test.go:214).
func seedUpgradableSeries(t *testing.T, st *store.Store, epCount int) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	sid, epIDs := seedSeries(t, st, true, epCount)
	for i := range epIDs {
		id := epIDs[i]
		if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
			MediaKind: "tv", EpisodeID: &id,
			RelativePath: fmt.Sprintf("e%d.mkv", i+1), QualityID: 7,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return sid, epIDs
}

func TestUpgradeSweepStopsAtPerSeriesLimit(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedUpgradableSeries(t, st, 4)
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	// Default config → limit 1.

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("upgrades must respect the per-series limit: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// Upgrades and missing-episode grabs share ONE budget.
func TestUpgradeSweepSharesBudgetWithInFlightGrab(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, epIDs := seedUpgradableSeries(t, st, 4)
	if _, err := st.EnqueueGrab(ctx, store.QueueItem{
		MediaKind: "tv", SeriesID: &sid, EpisodeIDs: []int64{epIDs[0]}, Status: store.QueueGrabbed,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("an in-flight grab must block upgrades for the same series: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// TestUpgradeSweepUngatedWhenLimitZero is the controller-addendum off-switch
// test: MaxConcurrentPerSeries = 0 must disable the gate entirely, so all 4
// upgradable episodes get grabbed. Without this test a mistaken clamp of
// limit <= 0 back to 1 would ship silently.
func TestUpgradeSweepUngatedWhenLimitZero(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedUpgradableSeries(t, st, 4)
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	c := DefaultConfig()
	c.MaxConcurrentPerSeries = 0
	if err := svc.SetConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || len(fe.reqs) != 4 {
		t.Fatalf("limit 0 disables the gate, want all 4 upgraded: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// TestUpgradeSweepIgnoresOtherSeriesInFlight is the controller-addendum
// cross-series isolation test: a DIFFERENT series (distinct TMDBID, since
// seedSeries hardcodes TMDBID 7 and download_queue.series_id has a real FK to
// series(id)) saturated with in-flight rows must not consume this series'
// budget. Each extra EnqueueGrab row needs a distinct ClientItemID or the
// UNIQUE(download_client_id, client_item_id) constraint fails the insert.
func TestUpgradeSweepIgnoresOtherSeriesInFlight(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedUpgradableSeries(t, st, 4)

	otherID, err := st.CreateSeries(ctx, store.Series{TMDBID: 999, Title: "Other Show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSeason(ctx, store.Season{SeriesID: otherID, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: otherID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	otherEps, err := st.ListEpisodes(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := st.EnqueueGrab(ctx, store.QueueItem{
			ClientItemID: fmt.Sprintf("other-%d", i),
			MediaKind:    "tv", SeriesID: &otherID, EpisodeIDs: []int64{otherEps[0].ID}, Status: store.QueueGrabbed,
		}); err != nil {
			t.Fatal(err)
		}
	}

	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	_ = sid

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("another series' in-flight rows must not affect this series' upgrade budget: n=%d reqs=%d", n, len(fe.reqs))
	}
}

func TestUpgradeSweepUpgradesMonitoredEpisodesOfUnmonitoredSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedUpgradableSeries(t, st, 4)
	if err := st.SetSeriesMonitored(ctx, sid, false); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: episodeReleases(4)}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("upgrades must follow episode monitoring, not the series flag: n=%d reqs=%d", n, len(fe.reqs))
	}
}

// An upgrade search hits the same loosely-matched q, so a better-scoring
// episode of a DIFFERENT show must not be swapped in over the real file.
func TestUpgradeEpisodeRejectsReleaseFromADifferentShow(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedUpgradableSeries(t, st, 1)
	fs := &fakeSearcher{releases: []provider.Release{{
		Title:       "The.Show.Trainer.Tour.S01E01.1080p.BluRay.x264-GRP",
		DownloadURL: "wrong", Protocol: provider.ProtocolUsenet,
	}}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("an upgrade must not take a different show's release: n=%d reqs=%+v", n, fe.reqs)
	}
}
