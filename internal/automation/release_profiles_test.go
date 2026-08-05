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