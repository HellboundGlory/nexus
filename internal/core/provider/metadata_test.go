package provider

import (
	"context"
	"testing"
)

// fakeMeta proves the interface is implementable and stable.
type fakeMeta struct{}

func (fakeMeta) SearchTV(context.Context, string) ([]MetadataResult, error)    { return nil, nil }
func (fakeMeta) SearchMovie(context.Context, string) ([]MetadataResult, error) { return nil, nil }
func (fakeMeta) TVDetails(context.Context, int) (SeriesMetadata, error)        { return SeriesMetadata{}, nil }
func (fakeMeta) MovieDetails(context.Context, int) (MovieMetadata, error) {
	return MovieMetadata{}, nil
}

func TestMetadataProviderShape(t *testing.T) {
	var p MetadataProvider = fakeMeta{}
	if _, err := p.SearchTV(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	s := SeriesMetadata{TMDBID: 1, Title: "T", Seasons: []SeasonMetadata{{
		SeasonNumber: 1, Episodes: []EpisodeMetadata{{SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"}},
	}}}
	if s.Seasons[0].Episodes[0].EpisodeNumber != 1 {
		t.Fatal("episode shape wrong")
	}
	r := MetadataResult{TMDBID: 2, Title: "M", Kind: KindMovie}
	if r.Kind != KindMovie {
		t.Fatal("kind wrong")
	}
}
