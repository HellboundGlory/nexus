package automation

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// noTagProfile returns a release profile with no tag scope (applies to every
// item) that ignores the given term.
func noTagProfile(t *testing.T, st *store.Store, name string, ignored []string) store.ReleaseProfile {
	t.Helper()
	p, err := st.CreateReleaseProfile(context.Background(), store.ReleaseProfile{
		Name: name, RequiredMode: "any", Ignored: ignored,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecideReleaseProfileRejectsIgnored(t *testing.T) {
	rel := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-DUB", Protocol: provider.ProtocolUsenet}
	rp := store.ReleaseProfile{Name: "No Dub", RequiredMode: "any", Ignored: []string{"dub"}}
	got := Decide([]provider.Release{rel}, provider.KindTV, hdProfile(), []store.ReleaseProfile{rp})
	if len(got) != 0 {
		t.Fatalf("release with ignored term must be dropped, got %d candidates", len(got))
	}
}

func TestDecideReleaseProfilePreferredTiebreak(t *testing.T) {
	plain := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-GRP", Protocol: provider.ProtocolUsenet}
	pref := provider.Release{Title: "The.Show.S01E01.1080p.BluRay.x264-REMUX", Protocol: provider.ProtocolUsenet}
	rp := store.ReleaseProfile{Name: "Prefer Remux", RequiredMode: "any", Preferred: []string{"remux"}}
	got := Decide([]provider.Release{plain, pref}, provider.KindTV, hdProfile(), []store.ReleaseProfile{rp})
	if len(got) != 2 {
		t.Fatalf("want 2 accepted, got %d", len(got))
	}
	if got[0].Release.Title != pref.Title {
		t.Fatalf("preferred-term release should rank first, got %q", got[0].Release.Title)
	}
}

func TestSearchMovieReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	id := seedMovie(t, st, true, true)
	tag, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieTags(ctx, id, []int64{tag.ID}); err != nil {
		t.Fatal(err)
	}
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-DUB", DownloadURL: "u1", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchMovie(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("release matching an ignored term must not be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

func TestRSSSyncReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	id := seedMovie(t, st, true, true)
	tag, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMovieTags(ctx, id, []int64{tag.ID}); err != nil {
		t.Fatal(err)
	}
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-DUB", DownloadURL: "u1", Protocol: provider.ProtocolUsenet, IndexerID: "nz"},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("RSS release matching an ignored term must not be grabbed: res=%+v reqs=%+v", res, fe.reqs)
	}
}

// TestSearchSeasonPackReleaseProfileRejects pins the gate on the season-pack
// branch of searchSeason: a fully-missing monitored season offered only a pack
// whose title matches an ignored term must not be grabbed.
func TestSearchSeasonPackReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sid, _ := seedSeries(t, st, true, 3) // season 1 fully missing, fully monitored
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01.1080p.BluRay.x264-DUB", DownloadURL: "pack", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchSeason(ctx, sid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("season pack matching an ignored term must not be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

// TestSearchEpisodeReleaseProfileRejects pins the gate on the searchEpisode path.
func TestSearchEpisodeReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	_, epIDs := seedSeries(t, st, true, 1)
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-DUB", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.SearchEpisode(ctx, epIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("episode matching an ignored term must not be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

// TestRSSSyncTVReleaseProfileRejects pins the gate on the RSS TV path: an
// episode release that resolves to a monitored TV series but matches an ignored
// term must not be grabbed.
func TestRSSSyncTVReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedSeries(t, st, true, 1)
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-DUB", DownloadURL: "e1", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Grabbed != 0 || len(fe.reqs) != 0 {
		t.Fatalf("RSS TV release matching an ignored term must not be grabbed: res=%+v reqs=%+v", res, fe.reqs)
	}
}

// TestUpgradeMovieReleaseProfileRejects pins the gate on the upgradeMovie path:
// a below-cutoff movie offered an upgrade whose release matches an ignored term
// must not be grabbed.
func TestUpgradeMovieReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	fileMovie(t, st, 7) // WEBDL-1080p(7) file, hdProfile cutoff 9 unmet
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-DUB", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("upgrade movie matching an ignored term must not be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

// TestUpgradeEpisodeReleaseProfileRejects pins the gate on the upgradeEpisode
// path: a below-cutoff episode offered a covering upgrade whose release matches
// an ignored term must not be grabbed.
func TestUpgradeEpisodeReleaseProfileRejects(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	seedUpgradableSeries(t, st, 1) // 1 episode with a WEBDL-1080p(7) file, cutoff 9 unmet
	noTagProfile(t, st, "No Dub", []string{"dub"})

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Show.S01E01.1080p.BluRay.x264-DUB", DownloadURL: "e1", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.UpgradeSweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(fe.reqs) != 0 {
		t.Fatalf("upgrade episode matching an ignored term must not be grabbed: n=%d reqs=%+v", n, fe.reqs)
	}
}

// TestRSSSyncTagScopedReleaseProfileAppliesOnlyToTaggedSeries pins the tag scope
// of a release profile: a profile assigned a tag applies only to items sharing
// that tag. The tagged series must reject the ignored-term release; the
// untagged series, whose profile set does not include the tag-scoped profile,
// is not restrained by it and (being otherwise grabbable) grabs its own release.
func TestRSSSyncTagScopedReleaseProfileAppliesOnlyToTaggedSeries(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	// Two distinct monitored series, each with one missing monitored episode.
	mkSeries := func(tmdb int, title string) int64 {
		t.Helper()
		p := hdProfile()
		p.Name = title // distinct profile name (quality_profiles.name is UNIQUE)
		prof, err := st.CreateQualityProfile(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		sid, err := st.CreateSeries(ctx, store.Series{TMDBID: tmdb, Title: title, Monitored: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSeriesQualityProfileID(ctx, sid, &prof.ID); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertSeason(ctx, store.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertEpisode(ctx, store.Episode{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true}); err != nil {
			t.Fatal(err)
		}
		return sid
	}
	tagged := mkSeries(5001, "Alpha Show")
	mkSeries(5002, "Beta Show") // untagged series

	tag, err := st.CreateTag(ctx, "anime")
	if err != nil {
		t.Fatal(err)
	}
	// Tag-scoped profile that ignores "dub"; it applies only where the tag matches.
	if _, err := st.CreateReleaseProfile(ctx, store.ReleaseProfile{
		Name: "Anime No Dub", RequiredMode: "any", Ignored: []string{"dub"}, TagIDs: []int64{tag.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesTags(ctx, tagged, []int64{tag.ID}); err != nil {
		t.Fatal(err)
	}

	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "Alpha.Show.S01E01.1080p.BluRay.x264-DUB", DownloadURL: "alpha", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
		{Title: "Beta.Show.S01E01.1080p.BluRay.x264-DUB", DownloadURL: "beta", Protocol: provider.ProtocolUsenet, Categories: []int{5040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	res, err := svc.RSSSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 || fe.reqs[0].DownloadURL != "beta" {
		t.Fatalf("tag-scoped profile must reject only the tagged series' release: res=%+v reqs=%+v", res, fe.reqs)
	}
	if res.Grabbed != 1 {
		t.Fatalf("want exactly the untagged series grabbed, got res=%+v", res)
	}
}