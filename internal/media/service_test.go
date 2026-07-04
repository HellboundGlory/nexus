package media

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// fakeProvider returns canned metadata; airDate controls the "future" monitor test.
type fakeProvider struct {
	series provider.SeriesMetadata
	movie  provider.MovieMetadata
}

func (f *fakeProvider) SearchTV(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.series.TMDBID, Title: f.series.Title, Kind: provider.KindTV}}, nil
}
func (f *fakeProvider) SearchMovie(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.movie.TMDBID, Title: f.movie.Title, Kind: provider.KindMovie}}, nil
}
func (f *fakeProvider) TVDetails(context.Context, int) (provider.SeriesMetadata, error) { return f.series, nil }
func (f *fakeProvider) MovieDetails(context.Context, int) (provider.MovieMetadata, error) { return f.movie, nil }

func newTestService(t *testing.T, fp *fakeProvider) (*Service, *store.Store) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	return NewService(st, fp), st
}

func sampleSeries() provider.SeriesMetadata {
	return provider.SeriesMetadata{
		TMDBID: 100, Title: "Show", Status: "Ended", FirstAired: "2020-01-01",
		Seasons: []provider.SeasonMetadata{{SeasonNumber: 1, Episodes: []provider.EpisodeMetadata{
			{SeasonNumber: 1, EpisodeNumber: 1, Title: "Aired", AirDate: "2020-01-01"},
			{SeasonNumber: 1, EpisodeNumber: 2, Title: "Future", AirDate: "2999-01-01"},
		}}},
	}
}

func TestAddSeriesMonitorFuture(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	rf, err := svc.AddRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rf.ID, MonitorOption: MonitorFuture})
	if err != nil {
		t.Fatal(err)
	}
	if !se.Monitored {
		t.Fatal("series should be monitored")
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	if len(eps) != 2 {
		t.Fatalf("want 2 episodes, got %d", len(eps))
	}
	// "future" → only the unaired episode is monitored.
	for _, e := range eps {
		if e.EpisodeNumber == 1 && e.Monitored {
			t.Fatal("aired episode should be unmonitored under 'future'")
		}
		if e.EpisodeNumber == 2 && !e.Monitored {
			t.Fatal("future episode should be monitored under 'future'")
		}
	}
}

func TestAddSeriesDuplicateRejected(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	ctx := context.Background()
	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll}); err != ErrAlreadyExists {
		t.Fatalf("want ErrAlreadyExists got %v", err)
	}
}

func TestAddSeriesInvalidRootFolder(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	bad := int64(999)
	if _, err := svc.AddSeries(context.Background(), AddSeriesRequest{TMDBID: 100, RootFolderID: &bad, MonitorOption: MonitorAll}); err != ErrInvalidRootFolder {
		t.Fatalf("want ErrInvalidRootFolder got %v", err)
	}
}

func TestAddMovie(t *testing.T) {
	fp := &fakeProvider{movie: provider.MovieMetadata{TMDBID: 200, Title: "Film", Year: 2020}}
	svc, _ := newTestService(t, fp)
	m, err := svc.AddMovie(context.Background(), AddMovieRequest{TMDBID: 200, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Film" || !m.Monitored {
		t.Fatalf("unexpected: %+v", m)
	}
}
