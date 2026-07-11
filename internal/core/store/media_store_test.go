package store

import (
	"context"
	"errors"
	"testing"
)

func TestRootFolderCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateRootFolder(ctx, "/data/tv")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := s.GetRootFolder(ctx, id)
	if err != nil || rf.Path != "/data/tv" {
		t.Fatalf("get: %+v err=%v", rf, err)
	}
	all, err := s.ListRootFolders(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.DeleteRootFolder(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRootFolder(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestSeriesAndEpisodeUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateSeries(ctx, Series{TMDBID: 100, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSeries(ctx, Series{TMDBID: 100, Title: "Dup"}); err == nil {
		t.Fatal("expected duplicate tmdb_id to error")
	}

	if err := s.UpsertSeason(ctx, Season{SeriesID: id, SeasonNumber: 1, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	// Upsert same episode twice: second call updates title, does not duplicate.
	ep := Episode{SeriesID: id, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot", Monitored: true}
	if err := s.UpsertEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	ep.Title = "Pilot (Extended)"
	if err := s.UpsertEpisode(ctx, ep); err != nil {
		t.Fatal(err)
	}
	eps, err := s.ListEpisodes(ctx, id)
	if err != nil || len(eps) != 1 || eps[0].Title != "Pilot (Extended)" {
		t.Fatalf("episodes: %+v err=%v", eps, err)
	}

	// Monitored preserved across a title-only re-upsert path is a Service concern;
	// here verify SetEpisodeMonitored + cascade helpers.
	if err := s.SetSeriesEpisodesMonitored(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	eps, _ = s.ListEpisodes(ctx, id)
	if eps[0].Monitored {
		t.Fatal("cascade to episodes failed")
	}

	// Cascade delete: deleting the series removes seasons + episodes.
	if err := s.DeleteSeries(ctx, id); err != nil {
		t.Fatal(err)
	}
	eps, _ = s.ListEpisodes(ctx, id)
	if len(eps) != 0 {
		t.Fatalf("expected episodes gone after series delete, got %d", len(eps))
	}
}

func TestMovieCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateMovie(ctx, Movie{TMDBID: 200, Title: "Film", SortTitle: "film", Year: 2020, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 200, Title: "Dup"}); err == nil {
		t.Fatal("expected duplicate tmdb_id to error")
	}
	m, err := s.GetMovie(ctx, id)
	if err != nil || m.Title != "Film" || m.Year != 2020 || !m.Monitored {
		t.Fatalf("get: %+v err=%v", m, err)
	}
	m.Title = "Film 2"
	if err := s.UpdateMovie(ctx, *m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMovieMonitored(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListMovies(ctx)
	if err != nil || len(all) != 1 || all[0].Title != "Film 2" || all[0].Monitored {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.DeleteMovie(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMovie(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestDeleteRootFolderInUseAndMissing(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	// Unused folder deletes cleanly.
	id, err := st.CreateRootFolder(ctx, "/data/unused")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.DeleteRootFolder(ctx, id); err != nil {
		t.Fatalf("delete unused: %v", err)
	}

	// Missing id → ErrNotFound.
	if err := st.DeleteRootFolder(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}

	// In-use folder → ErrRootFolderInUse.
	inUse, err := st.CreateRootFolder(ctx, "/data/inuse")
	if err != nil {
		t.Fatalf("create inuse: %v", err)
	}
	if _, err := st.CreateSeries(ctx, Series{TMDBID: 555, Title: "Ref", RootFolderID: &inUse}); err != nil {
		t.Fatalf("create series: %v", err)
	}
	if err := st.DeleteRootFolder(ctx, inUse); !errors.Is(err, ErrRootFolderInUse) {
		t.Fatalf("delete in-use: want ErrRootFolderInUse, got %v", err)
	}
}
