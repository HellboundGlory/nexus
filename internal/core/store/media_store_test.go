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

func TestCalendarQueries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sid, err := s.CreateSeries(ctx, Series{TMDBID: 1, Title: "Show", SortTitle: "show", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	unmon, err := s.CreateSeries(ctx, Series{TMDBID: 2, Title: "Hidden", SortTitle: "hidden", Monitored: false})
	if err != nil {
		t.Fatal(err)
	}

	eps := []Episode{
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 1, Title: "StartEdge", AirDate: "2026-07-10", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 2, Title: "EndEdge", AirDate: "2026-07-31", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 3, Title: "AfterEnd", AirDate: "2026-08-01", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 4, Title: "NoDate", AirDate: "", Monitored: true},
		{SeriesID: sid, SeasonNumber: 1, EpisodeNumber: 5, Title: "Unmon", AirDate: "2026-07-15", Monitored: false},
		{SeriesID: unmon, SeasonNumber: 1, EpisodeNumber: 1, Title: "HiddenSeries", AirDate: "2026-07-15", Monitored: true},
	}
	for _, e := range eps {
		if err := s.UpsertEpisode(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.CalendarEpisodes(ctx, "2026-07-10", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 episodes got %d: %+v", len(got), got)
	}
	if got[0].Title != "StartEdge" || got[1].Title != "EndEdge" {
		t.Fatalf("order/content: %+v", got)
	}
	if got[0].SeriesTitle != "Show" {
		t.Fatalf("series title join: %+v", got[0])
	}

	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 10, Title: "In", SortTitle: "in", Year: 2026, ReleaseDate: "2026-07-20", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 11, Title: "Out", SortTitle: "out", Year: 2026, ReleaseDate: "2026-08-15", Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 12, Title: "Unmon", SortTitle: "unmon", Year: 2026, ReleaseDate: "2026-07-20", Monitored: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMovie(ctx, Movie{TMDBID: 13, Title: "NoDate", SortTitle: "nodate", Year: 2026, ReleaseDate: "", Monitored: true}); err != nil {
		t.Fatal(err)
	}

	gm, err := s.CalendarMovies(ctx, "2026-07-10", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(gm) != 1 || gm[0].Title != "In" {
		t.Fatalf("want 1 movie In got %+v", gm)
	}
}
