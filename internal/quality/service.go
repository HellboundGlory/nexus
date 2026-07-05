package quality

import (
	"context"
	"errors"
	"strings"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrInvalidProfile is returned when a profile fails validation.
var ErrInvalidProfile = errors.New("quality: invalid profile")

// Service owns quality-profile CRUD with validation over the store.
type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service { return &Service{store: st} }

func validateProfile(p store.QualityProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrInvalidProfile
	}
	if len(p.Items) == 0 {
		return ErrInvalidProfile
	}
	allowed := map[int]bool{}
	for _, it := range p.Items {
		if _, ok := DefinitionByID(it.QualityID); !ok {
			return ErrInvalidProfile
		}
		if it.Allowed {
			allowed[it.QualityID] = true
		}
	}
	if _, ok := DefinitionByID(p.CutoffQualityID); !ok {
		return ErrInvalidProfile
	}
	if !allowed[p.CutoffQualityID] {
		return ErrInvalidProfile
	}
	return nil
}

func (s *Service) CreateProfile(ctx context.Context, p store.QualityProfile) (store.QualityProfile, error) {
	if err := validateProfile(p); err != nil {
		return store.QualityProfile{}, err
	}
	return s.store.CreateQualityProfile(ctx, p)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (store.QualityProfile, error) {
	return s.store.GetQualityProfile(ctx, id)
}

func (s *Service) ListProfiles(ctx context.Context) ([]store.QualityProfile, error) {
	return s.store.ListQualityProfiles(ctx)
}

func (s *Service) UpdateProfile(ctx context.Context, p store.QualityProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	return s.store.UpdateQualityProfile(ctx, p)
}

func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	return s.store.DeleteQualityProfile(ctx, id)
}
