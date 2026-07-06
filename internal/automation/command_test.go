package automation

import (
	"context"
	"testing"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type nopReporter struct{}

func (nopReporter) Progress(int, string) {}

func TestSearchMovieCommandRuns(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewSearchMovieCommand(svc, id)
	if cmd.Name() != "SearchMovie" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("command should have grabbed one, got %d", len(fe.reqs))
	}
}

func TestRSSSyncCommandRuns(t *testing.T) {
	st := newStore(t)
	seedMovie(t, st, true, true)
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet, Categories: []int{2040}},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewRSSSyncCommand(svc)
	if cmd.Name() != "RSSSync" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("RSS command should have grabbed one, got %d", len(fe.reqs))
	}
}

func TestUpgradeSearchCommandRuns(t *testing.T) {
	st := newStore(t)
	id := seedMovie(t, st, true, true)
	if _, err := st.UpsertMediaFile(context.Background(), store.MediaFile{
		MediaKind: "movie", MovieID: &id, RelativePath: "m.mkv", QualityID: 7,
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "blu", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)
	cmd := NewUpgradeSearchCommand(svc)
	if cmd.Name() != "UpgradeSearch" {
		t.Fatalf("bad name %q", cmd.Name())
	}
	if err := cmd.Run(context.Background(), nopReporter{}); err != nil {
		t.Fatal(err)
	}
	if len(fe.reqs) != 1 {
		t.Fatalf("upgrade command should have grabbed one, got %d", len(fe.reqs))
	}
}

func TestMissingSweepRespectsBatchAndSkipsFiled(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	// Two monitored missing movies + one monitored movie that already has a file.
	seedMovie(t, st, true, true)
	seedMovie(t, st, true, true)
	filed := seedMovie(t, st, true, true)
	if _, err := st.UpsertMediaFile(ctx, store.MediaFile{MediaKind: "movie", MovieID: &filed, RelativePath: "m.mkv", QualityID: 9}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSearcher{releases: []provider.Release{
		{Title: "The.Film.2020.1080p.BluRay.x264-GRP", DownloadURL: "u", Protocol: provider.ProtocolUsenet},
	}}
	fe := &fakeEnqueuer{}
	svc := NewService(st, fs, fe, nil)

	n, err := svc.MissingSweep(ctx, 1) // batch of 1 → only the first missing movie
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(fe.reqs) != 1 {
		t.Fatalf("batch=1 should process exactly one target: n=%d reqs=%d", n, len(fe.reqs))
	}
}
