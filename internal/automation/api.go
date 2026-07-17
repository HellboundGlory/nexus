package automation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/store"
)

// Dispatcher enqueues a command onto the worker pool, returning its task id.
// Satisfied by *command.Manager.
type Dispatcher interface {
	Enqueue(command.Command) (string, error)
}

type API struct {
	svc      *Service
	dispatch Dispatcher
}

func NewAPI(svc *Service, d Dispatcher) *API { return &API{svc: svc, dispatch: d} }

func (a *API) Mount(r chi.Router) {
	r.Route("/automation", func(r chi.Router) {
		r.Post("/search/movie/{id}", a.searchMovie)
		r.Post("/search/series/{id}", a.searchSeries)
		r.Post("/search/series/{id}/season/{n}", a.searchSeason)
		r.Post("/search/episode/{id}", a.searchEpisode)
		// Interactive reads are synchronous 200s, deliberately unlike the
		// fire-and-forget 202s above: the modal must block on real results, and
		// prod testing showed a 202 makes "nothing happened" ambiguous.
		r.Get("/search/movie/{id}/interactive", a.interactiveMovie)
		r.Get("/search/series/{id}/season/{n}/interactive", a.interactiveSeason)
		r.Get("/search/episode/{id}/interactive", a.interactiveEpisode)
		r.Route("/config", func(r chi.Router) {
			r.Get("/", a.getConfig)
			r.Put("/", a.putConfig)
		})
	})
}

func pathInt64(r *http.Request, key string) (int64, bool) {
	v, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// accept validates the target exists (404 if not) then enqueues the command and
// writes 202 with the task id.
func (a *API) accept(w http.ResponseWriter, exists error, cmd command.Command) {
	if errors.Is(exists, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if exists != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	id, err := a.dispatch.Enqueue(cmd)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to dispatch")
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"taskId": id})
}

func (a *API) searchMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetMovie(r.Context(), id)
	a.accept(w, err, NewSearchMovieCommand(a.svc, id))
}

func (a *API) searchSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetSeries(r.Context(), id)
	a.accept(w, err, NewSearchSeriesCommand(a.svc, id))
}

func (a *API) searchSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	n, okN := pathInt64(r, "n")
	if !ok || !okN {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id or season")
		return
	}
	_, err := a.svc.store.GetSeries(r.Context(), id)
	a.accept(w, err, NewSearchSeasonCommand(a.svc, id, int(n)))
}

func (a *API) searchEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, err := a.svc.store.GetEpisode(r.Context(), id)
	a.accept(w, err, NewSearchEpisodeCommand(a.svc, id))
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.Config(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) putConfig(w http.ResponseWriter, r *http.Request) {
	var c Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetConfig(r.Context(), c); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to save config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

// writeInteractive maps the interactive entry points' errors onto the API's
// error vocabulary and writes the result.
func (a *API) writeInteractive(w http.ResponseWriter, res InteractiveResult, err error) {
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, res)
	case errors.Is(err, ErrNoProfile):
		api.WriteError(w, http.StatusBadRequest, "no_profile", "assign a quality profile before searching")
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "search failed")
	}
}

func (a *API) interactiveMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	res, err := a.svc.InteractiveSearchMovie(r.Context(), id)
	a.writeInteractive(w, res, err)
}

func (a *API) interactiveSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	n, okN := pathInt64(r, "n")
	if !ok || !okN {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id or season")
		return
	}
	res, err := a.svc.InteractiveSearchSeason(r.Context(), id, int(n))
	a.writeInteractive(w, res, err)
}

func (a *API) interactiveEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	res, err := a.svc.InteractiveSearchEpisode(r.Context(), id)
	a.writeInteractive(w, res, err)
}
