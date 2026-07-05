package quality

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/parsing"
)

type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Mount(r chi.Router) {
	r.Get("/quality/definitions", a.definitions)
	r.Route("/qualityprofile", func(r chi.Router) {
		r.Get("/", a.listProfiles)
		r.Post("/", a.createProfile)
		r.Get("/{id}", a.getProfile)
		r.Put("/{id}", a.updateProfile)
		r.Delete("/{id}", a.deleteProfile)
	})
	r.Post("/parse", a.parse)
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
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "profile not found")
	case errors.Is(err, store.ErrProfileInUse):
		api.WriteError(w, http.StatusConflict, "conflict", "profile is in use")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) definitions(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, Definitions())
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.ListProfiles(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list profiles")
		return
	}
	if rows == nil {
		rows = []store.QualityProfile{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var p store.QualityProfile
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
	var p store.QualityProfile
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

type parseBody struct {
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	ProfileID *int64 `json:"profileId"`
}

func (a *API) parse(w http.ResponseWriter, r *http.Request) {
	var b parseBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.Title == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "title is required")
		return
	}
	kind := provider.KindMovie
	if b.Kind == "tv" {
		kind = provider.KindTV
	}
	parsed := parsing.Parse(b.Title, kind)
	resolved := Resolve(parsed)

	resp := map[string]any{"parsed": parsed, "quality": resolved}
	if b.ProfileID != nil {
		prof, err := a.svc.GetProfile(r.Context(), *b.ProfileID)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		d := Decide(parsed, prof)
		resp["decision"] = d
	}
	api.WriteJSON(w, http.StatusOK, resp)
}
