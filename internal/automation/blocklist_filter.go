package automation

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/store"
)

// filterBlocklisted drops candidates whose normalized release title is blocked.
func filterBlocklisted(cands []Candidate, blocked map[string]bool) []Candidate {
	if len(blocked) == 0 {
		return cands
	}
	out := cands[:0:0]
	for _, c := range cands {
		if !blocked[store.NormReleaseTitle(c.Release.Title)] {
			out = append(out, c)
		}
	}
	return out
}

// ResearchMovie / ResearchEpisode satisfy importing.Researcher (re-search after
// a failed download). They reuse the existing search paths.
func (s *Service) ResearchMovie(ctx context.Context, movieID int64) error {
	_, err := s.SearchMovie(ctx, movieID)
	return err
}

func (s *Service) ResearchEpisode(ctx context.Context, episodeID int64) error {
	_, err := s.SearchEpisode(ctx, episodeID)
	return err
}

func (s *Service) ResearchSeries(ctx context.Context, seriesID int64) error {
	_, err := s.SearchSeries(ctx, seriesID)
	return err
}
