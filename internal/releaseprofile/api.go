// Package releaseprofile exposes release-profile CRUD over HTTP. Release
// profiles are reusable named rules scoped to media by tag that filter and
// score releases by substring terms on the raw release title.
package releaseprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrInvalidProfile is returned when a release profile fails validation.
var ErrInvalidProfile = errors.New("releaseprofile: invalid profile")

// Service owns release-profile CRUD with validation over the store.
type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service { return &Service{store: st} }

func validateProfile(p store.ReleaseProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrInvalidProfile
	}
	if p.RequiredMode != "any" && p.RequiredMode != "all" {
		return ErrInvalidProfile
	}
	if len(p.RequiredAny) == 0 && len(p.RequiredAll) == 0 && len(p.Ignored) == 0 && len(p.Preferred) == 0 {
		return ErrInvalidProfile
	}
	return nil
}

func (s *Service) CreateProfile(ctx context.Context, p store.ReleaseProfile) (store.ReleaseProfile, error) {
	if err := validateProfile(p); err != nil {
		return store.ReleaseProfile{}, err
	}
	return s.store.CreateReleaseProfile(ctx, p)
}

func (s *Service) GetProfile(ctx context.Context, id int64) (store.ReleaseProfile, error) {
	return s.store.GetReleaseProfile(ctx, id)
}

func (s *Service) ListProfiles(ctx context.Context) ([]store.ReleaseProfile, error) {
	return s.store.ListReleaseProfiles(ctx)
}

func (s *Service) UpdateProfile(ctx context.Context, p store.ReleaseProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	return s.store.UpdateReleaseProfile(ctx, p)
}

func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	return s.store.DeleteReleaseProfile(ctx, id)
}

type API struct {
	svc *Service
}

func NewAPI(st *store.Store) *API { return &API{svc: NewService(st)} }

func (a *API) Mount(r chi.Router) {
	r.Route("/releaseprofile", func(r chi.Router) {
		r.Get("/", a.listProfiles)
		r.Post("/", a.createProfile)
		r.Get("/{id}", a.getProfile)
		r.Put("/{id}", a.updateProfile)
		r.Delete("/{id}", a.deleteProfile)
	})
}

func profileID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidProfile):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, store.ErrTagNotFound):
		api.WriteError(w, http.StatusBadRequest, "bad_request", "unknown tag id")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "release profile not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.ListProfiles(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list release profiles")
		return
	}
	if rows == nil {
		rows = []store.ReleaseProfile{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var p store.ReleaseProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	created, err := a.svc.CreateProfile(r.Context(), p)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, created)
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	p, err := a.svc.GetProfile(r.Context(), id)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, p)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	var p store.ReleaseProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	p.ID = id
	if err := a.svc.UpdateProfile(r.Context(), p); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	if err := a.svc.DeleteProfile(r.Context(), id); err != nil {
		writeProfileError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}