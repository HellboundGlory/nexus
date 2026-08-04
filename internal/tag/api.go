package tag

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
}

func NewAPI(st *store.Store) *API { return &API{store: st} }

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Route("/tag", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Put("/{id}", a.rename)
		r.Delete("/{id}", a.delete)
	})
}

type labelBody struct {
	Label string `json:"label"`
}

func tagID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func writeTagError(w http.ResponseWriter, err error) {
	var inUse *store.TagInUseError
	switch {
	case errors.As(err, &inUse):
		api.WriteError(w, http.StatusConflict, "tag_in_use",
			fmt.Sprintf("tag is in use by %d series and %d movies", inUse.SeriesCount, inUse.MovieCount))
	case errors.Is(err, store.ErrTagExists):
		api.WriteError(w, http.StatusConflict, "tag_exists", "a tag with that label already exists")
	case errors.Is(err, store.ErrInvalidTag):
		api.WriteError(w, http.StatusBadRequest, "bad_request", "label must not be empty")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "tag not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListTags(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list tags")
		return
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var b labelBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	created, err := a.store.CreateTag(r.Context(), b.Label)
	if err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, created)
}

func (a *API) rename(w http.ResponseWriter, r *http.Request) {
	id, ok := tagID(w, r)
	if !ok {
		return
	}
	var b labelBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.store.RenameTag(r.Context(), id, b.Label); err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := tagID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTag(r.Context(), id); err != nil {
		writeTagError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
