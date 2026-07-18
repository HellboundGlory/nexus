package media

import (
	"context"
	"errors"
	"strconv"

	"github.com/hellboundg/nexus/internal/core/store"
)

const (
	keyDefaultMovieRoot    = "defaults.movie.rootFolderId"
	keyDefaultMovieProfile = "defaults.movie.qualityProfileId"
	keyDefaultTVRoot       = "defaults.tv.rootFolderId"
	keyDefaultTVProfile    = "defaults.tv.qualityProfileId"
)

// KindDefaults is the add-time default root folder and quality profile for one
// media kind. Each is nil when unset (JSON null on the wire).
type KindDefaults struct {
	RootFolderID     *int64 `json:"rootFolderId"`
	QualityProfileID *int64 `json:"qualityProfileId"`
}

// MediaDefaults is the per-kind add defaults, stored as four ids in the generic
// settings table.
type MediaDefaults struct {
	Movie KindDefaults `json:"movie"`
	TV    KindDefaults `json:"tv"`
}

// GetMediaDefaults reads the four stored ids and validates each against the live
// set. A stored id whose folder/profile has since been deleted is returned as nil
// (never a dangling id — a deleted default must not pre-select a phantom option).
func (s *Service) GetMediaDefaults(ctx context.Context) (MediaDefaults, error) {
	movieRoot, err := s.resolveDefault(ctx, keyDefaultMovieRoot, s.rootFolderExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	movieProfile, err := s.resolveDefault(ctx, keyDefaultMovieProfile, s.qualityProfileExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	tvRoot, err := s.resolveDefault(ctx, keyDefaultTVRoot, s.rootFolderExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	tvProfile, err := s.resolveDefault(ctx, keyDefaultTVProfile, s.qualityProfileExists)
	if err != nil {
		return MediaDefaults{}, err
	}
	return MediaDefaults{
		Movie: KindDefaults{RootFolderID: movieRoot, QualityProfileID: movieProfile},
		TV:    KindDefaults{RootFolderID: tvRoot, QualityProfileID: tvProfile},
	}, nil
}

// resolveDefault reads one setting key and returns the stored id only if it
// parses AND still exists. Missing/empty/unparseable/deleted → nil (no default).
// A real store error (not "not found") propagates.
func (s *Service) resolveDefault(ctx context.Context, key string, exists func(context.Context, int64) (bool, error)) (*int64, error) {
	raw, found, err := s.store.GetSetting(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found || raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, nil // corrupt value → treat as unset
	}
	ok, err := exists(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (s *Service) rootFolderExists(ctx context.Context, id int64) (bool, error) {
	_, err := s.store.GetRootFolder(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) qualityProfileExists(ctx context.Context, id int64) (bool, error) {
	_, err := s.store.GetQualityProfile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetMediaDefaults validates every non-nil id first, then writes all four keys.
// Validation-before-write makes it all-or-nothing: an unknown id fails the whole
// PUT and mutates nothing. A nil id is stored as "" (read back as unset).
func (s *Service) SetMediaDefaults(ctx context.Context, d MediaDefaults) error {
	if err := s.validateRootFolder(ctx, d.Movie.RootFolderID); err != nil {
		return err
	}
	if err := s.validateQualityProfile(ctx, d.Movie.QualityProfileID); err != nil {
		return err
	}
	if err := s.validateRootFolder(ctx, d.TV.RootFolderID); err != nil {
		return err
	}
	if err := s.validateQualityProfile(ctx, d.TV.QualityProfileID); err != nil {
		return err
	}
	for _, kv := range []struct {
		key string
		id  *int64
	}{
		{keyDefaultMovieRoot, d.Movie.RootFolderID},
		{keyDefaultMovieProfile, d.Movie.QualityProfileID},
		{keyDefaultTVRoot, d.TV.RootFolderID},
		{keyDefaultTVProfile, d.TV.QualityProfileID},
	} {
		v := ""
		if kv.id != nil {
			v = strconv.FormatInt(*kv.id, 10)
		}
		if err := s.store.SetSetting(ctx, kv.key, v); err != nil {
			return err
		}
	}
	return nil
}
