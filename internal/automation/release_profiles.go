package automation

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/store"
)

// applicableReleaseProfiles returns the release profiles that apply to an item
// with the given tag ids: those whose TagIDs intersect the item's tags, plus
// any profile with no tags (a no-tag profile applies to everything).
func applicableReleaseProfiles(all []store.ReleaseProfile, itemTagIDs []int64) []store.ReleaseProfile {
	itemSet := map[int64]struct{}{}
	for _, id := range itemTagIDs {
		itemSet[id] = struct{}{}
	}
	var out []store.ReleaseProfile
	for _, rp := range all {
		if len(rp.TagIDs) == 0 {
			out = append(out, rp)
			continue
		}
		for _, tagID := range rp.TagIDs {
			if _, ok := itemSet[tagID]; ok {
				out = append(out, rp)
				break
			}
		}
	}
	return out
}

// releaseProfilesForSeries returns the release profiles applicable to a series.
func (s *Service) releaseProfilesForSeries(ctx context.Context, seriesID int64) ([]store.ReleaseProfile, error) {
	all, err := s.store.ListReleaseProfiles(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.store.TagsForSeries(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	return applicableReleaseProfiles(all, tags), nil
}

// releaseProfilesForMovie returns the release profiles applicable to a movie.
func (s *Service) releaseProfilesForMovie(ctx context.Context, movieID int64) ([]store.ReleaseProfile, error) {
	all, err := s.store.ListReleaseProfiles(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.store.TagsForMovie(ctx, movieID)
	if err != nil {
		return nil, err
	}
	return applicableReleaseProfiles(all, tags), nil
}

// rpsForEntity resolves the release profiles applicable to one entity from the
// batch maps built once per RSS sweep. byEntity already encodes the tag
// intersection (a profile id appears under an entity id only when the profile's
// tag matches the entity's tag); a no-tag profile applies to everything.
func rpsForEntity(all []store.ReleaseProfile, byEntity map[int64][]int64, entityID int64) []store.ReleaseProfile {
	ids := map[int64]struct{}{}
	for _, id := range byEntity[entityID] {
		ids[id] = struct{}{}
	}
	var out []store.ReleaseProfile
	for _, rp := range all {
		if len(rp.TagIDs) == 0 {
			out = append(out, rp)
			continue
		}
		if _, ok := ids[rp.ID]; ok {
			out = append(out, rp)
		}
	}
	return out
}
