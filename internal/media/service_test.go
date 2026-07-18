package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// fakeProvider returns canned metadata; airDate controls the "future" monitor test.
type fakeProvider struct {
	series provider.SeriesMetadata
	movies provider.MovieMetadata
}

func (f *fakeProvider) SearchTV(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.series.TMDBID, Title: f.series.Title, Kind: provider.KindTV}}, nil
}
func (f *fakeProvider) SearchMovie(context.Context, string) ([]provider.MetadataResult, error) {
	return []provider.MetadataResult{{TMDBID: f.movies.TMDBID, Title: f.movies.Title, Kind: provider.KindMovie}}, nil
}
func (f *fakeProvider) TVDetails(context.Context, int) (provider.SeriesMetadata, error) { return f.series, nil }
func (f *fakeProvider) MovieDetails(context.Context, int) (provider.MovieMetadata, error) { return f.movies, nil }

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

func sampleMovies() provider.MovieMetadata {
	return provider.MovieMetadata{TMDBID: 200, Title: "Film", Year: 2020}
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

func TestAddSeriesWithQualityProfile(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	rf, err := svc.AddRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	prof, err := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	se, err := svc.AddSeries(ctx, AddSeriesRequest{
		TMDBID: 100, RootFolderID: &rf.ID, MonitorOption: MonitorAll, QualityProfileID: &prof.ID,
	})
	if err != nil {
		t.Fatalf("AddSeries: %v", err)
	}
	if se.QualityProfileID == nil || *se.QualityProfileID != prof.ID {
		t.Fatalf("qualityProfileId = %v, want %d", se.QualityProfileID, prof.ID)
	}
}

func TestAddSeriesUnknownQualityProfileRejected(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, _ := newTestService(t, fp)
	ctx := context.Background()
	bogus := int64(99999)

	_, err := svc.AddSeries(ctx, AddSeriesRequest{
		TMDBID: 100, MonitorOption: MonitorAll, QualityProfileID: &bogus,
	})
	if !errors.Is(err, ErrInvalidQualityProfile) {
		t.Fatalf("err = %v, want ErrInvalidQualityProfile", err)
	}
}

func TestAddMovie(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, _ := newTestService(t, fp)
	m, err := svc.AddMovie(context.Background(), AddMovieRequest{TMDBID: 200, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Film" || !m.Monitored {
		t.Fatalf("unexpected: %+v", m)
	}
}

func TestAddMovieWithQualityProfile(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	rf, err := svc.AddRootFolder(ctx, "/data/movies")
	if err != nil {
		t.Fatal(err)
	}
	prof, err := st.CreateQualityProfile(ctx, store.QualityProfile{
		Name: "HD", CutoffQualityID: 9, UpgradeAllowed: true,
		Items: []store.QualityProfileItem{{QualityID: 9, Allowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 200, RootFolderID: &rf.ID, Monitored: true, QualityProfileID: &prof.ID,
	})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	if m.QualityProfileID == nil || *m.QualityProfileID != prof.ID {
		t.Fatalf("qualityProfileId = %v, want %d", m.QualityProfileID, prof.ID)
	}
}

func TestAddMovieUnknownQualityProfileRejected(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, _ := newTestService(t, fp)
	ctx := context.Background()
	bogus := int64(99999)

	_, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 200, Monitored: true, QualityProfileID: &bogus,
	})
	if !errors.Is(err, ErrInvalidQualityProfile) {
		t.Fatalf("err = %v, want ErrInvalidQualityProfile", err)
	}
}

func TestAddMovieWithoutQualityProfileStaysNil(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, _ := newTestService(t, fp)
	ctx := context.Background()

	m, err := svc.AddMovie(ctx, AddMovieRequest{
		TMDBID: 200, Monitored: true, // QualityProfileID omitted
	})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	if m.QualityProfileID != nil {
		t.Fatalf("qualityProfileId = %v, want nil (additive guarantee)", m.QualityProfileID)
	}
}

func TestRefreshReconciles(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorNone})
	if err != nil {
		t.Fatal(err)
	}
	// User monitors episode 1 manually.
	eps, _ := st.ListEpisodes(ctx, se.ID)
	var ep1 int64
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			ep1 = e.ID
		}
	}
	if err := svc.SetEpisodeMonitored(ctx, se.ID, ep1, true); err != nil {
		t.Fatal(err)
	}

	// Upstream now: episode 1 title changed + a new episode 3 appears.
	updated := sampleSeries()
	updated.Title = "Show (Renamed)"
	updated.Seasons[0].Episodes[0].Title = "Aired (v2)"
	updated.Seasons[0].Episodes = append(updated.Seasons[0].Episodes, provider.EpisodeMetadata{
		SeasonNumber: 1, EpisodeNumber: 3, Title: "New", AirDate: "2020-02-01",
	})
	fp.series = updated

	if err := svc.RefreshSeries(ctx, se.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSeries(ctx, se.ID)
	if got.Title != "Show (Renamed)" {
		t.Fatalf("title not refreshed: %q", got.Title)
	}
	eps, _ = st.ListEpisodes(ctx, se.ID)
	if len(eps) != 3 {
		t.Fatalf("want 3 episodes after refresh, got %d", len(eps))
	}
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			if e.Title != "Aired (v2)" {
				t.Fatalf("ep1 title not updated: %q", e.Title)
			}
			if !e.Monitored {
				t.Fatal("refresh must PRESERVE user's monitored=true on ep1")
			}
		}
	}
}

func TestSetSeriesMonitoredCascades(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	se, _ := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, MonitorOption: MonitorAll})

	if err := svc.SetSeriesMonitored(ctx, se.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSeries(ctx, se.ID)
	if got.Monitored {
		t.Fatal("series still monitored")
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	for _, e := range eps {
		if e.Monitored {
			t.Fatal("series unmonitor should cascade to episodes")
		}
	}
}

func TestDeleteMovieFileRemovesRowAndDisk(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	if err != nil {
		t.Fatal(err)
	}
	rel := "Film (2020)/Film.2020.1080p.mkv"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: rel, Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("DeleteMovieFile: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
	f, err := st.MediaFileForMovie(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("media file row should be deleted")
	}
}

func TestDeleteMovieFileIdempotentWhenNoFile(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("want nil for no-file, got %v", err)
	}
}

func TestDeleteMovieFileRemovesRowWhenRootUnresolvable(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	// No RootFolderID set → disk step skipped, row still deleted.
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "nowhere/x.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMovieFile(ctx, mid); err != nil {
		t.Fatalf("DeleteMovieFile: %v", err)
	}
	f, err := st.MediaFileForMovie(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("row should be deleted even when root unresolvable")
	}
}

func TestDeleteMovieWithDiskRemovesFolderAndRows(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "Film (2020)")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Film.2020.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/Film.2020.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("folder should be gone, stat err = %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should be deleted, got %v", err)
	}
}

func TestDeleteMovieWithoutDiskKeepsFolder(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	folder := filepath.Join(root, "Film (2020)")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "f.mkv"), []byte("x"), 0o644)
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "Film (2020)/f.mkv", Size: 1, QualityID: 9})

	if err := svc.DeleteMovie(ctx, mid, false); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("folder should remain when deleteFiles=false, got %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should still be deleted from DB, got %v", err)
	}
}

func TestDeleteMovieWithDiskNoFileSkipsDisk(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film"})
	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("want nil for no-file, got %v", err)
	}
	if _, err := st.GetMovie(ctx, mid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("movie should be deleted, got %v", err)
	}
}

func TestDeleteMovieContainmentGuardRejectsEscape(t *testing.T) {
	fp := &fakeProvider{movies: sampleMovies()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sentinel OUTSIDE the root that a naive RemoveAll of "../victim" would hit.
	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	rid, _ := st.CreateRootFolder(ctx, root)
	mid, _ := st.CreateMovie(ctx, store.Movie{TMDBID: 200, Title: "Film", RootFolderID: &rid})
	st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &mid, RelativePath: "../victim/f.mkv", Size: 1, QualityID: 9})

	if err := svc.DeleteMovie(ctx, mid, true); err != nil {
		t.Fatalf("DeleteMovie: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("containment guard failed — victim outside root was removed: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root itself must survive: %v", err)
	}
}

func TestDeleteSeriesWithDiskRemovesFolderAndRows(t *testing.T) {
	fp := &fakeProvider{series: sampleSeries()}
	svc, st := newTestService(t, fp)
	ctx := context.Background()

	root := t.TempDir()
	rid, err := st.CreateRootFolder(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	se, err := svc.AddSeries(ctx, AddSeriesRequest{TMDBID: 100, RootFolderID: &rid, MonitorOption: MonitorAll})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := st.ListEpisodes(ctx, se.ID)
	if len(eps) == 0 {
		t.Fatal("no episodes")
	}
	seasonDir := filepath.Join(root, "Show", "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(seasonDir, "E01.mkv"), []byte("x"), 0o644)
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{
		MediaKind: "episode", EpisodeID: &eps[0].ID, RelativePath: "Show/Season 01/E01.mkv", Size: 1, QualityID: 9,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteSeries(ctx, se.ID, true); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show")); !os.IsNotExist(err) {
		t.Fatalf("series folder should be gone, stat err = %v", err)
	}
	if _, err := st.GetSeries(ctx, se.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("series should be deleted, got %v", err)
	}
}
