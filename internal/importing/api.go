package importing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
)

type API struct{ svc *Service }

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Mount(r chi.Router) {
	r.Route("/queue", func(r chi.Router) {
		r.Get("/", a.listQueue)
		r.Post("/", a.enqueue)
		r.Delete("/{id}", a.deleteQueue)
		r.Post("/{id}/import", a.importItem)
	})
	r.Get("/history", a.history)
	r.Route("/blocklist", func(r chi.Router) {
		r.Get("/", a.listBlocklist)
		r.Delete("/{id}", a.removeBlocklist)
	})
	r.Route("/config/naming", func(r chi.Router) {
		r.Get("/", a.getNaming)
		r.Put("/", a.putNaming)
	})
}

func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func (a *API) listQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListQueue(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list queue")
		return
	}
	if rows == nil {
		rows = []store.QueueItem{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

type enqueueBody struct {
	DownloadURL string             `json:"downloadUrl"`
	Title       string             `json:"title"`
	Protocol    provider.Protocol  `json:"protocol"`
	IndexerID   string             `json:"indexerId"`
	ClientID    string             `json:"clientId"`
	MediaKind   provider.MediaKind `json:"mediaKind"`
	SeriesID    int64              `json:"seriesId"`
	EpisodeIDs  []int64            `json:"episodeIds"`
	MovieID     int64              `json:"movieId"`
}

func (a *API) enqueue(w http.ResponseWriter, r *http.Request) {
	var b enqueueBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.Title == "" || b.DownloadURL == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "downloadUrl and title are required")
		return
	}
	q, err := a.svc.Enqueue(r.Context(), EnqueueRequest{
		DownloadURL: b.DownloadURL, Title: b.Title, Protocol: b.Protocol, IndexerID: b.IndexerID,
		ClientID: b.ClientID, MediaKind: b.MediaKind, SeriesID: b.SeriesID, EpisodeIDs: b.EpisodeIDs, MovieID: b.MovieID,
	})
	if err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, q)
}

func (a *API) deleteQueue(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := a.svc.store.DeleteQueueItem(r.Context(), id); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) importItem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := a.svc.ImportItem(r.Context(), id); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := a.svc.store.ListHistory(r.Context(), limit)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list history")
		return
	}
	if rows == nil {
		rows = []store.HistoryEvent{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

type blocklistDTO struct {
	store.Blocklist
	Title string `json:"title"` // movie/series display title, "" if deleted
}

func (a *API) listBlocklist(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListBlocklist(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list blocklist")
		return
	}
	out := make([]blocklistDTO, 0, len(rows))
	for _, bl := range rows {
		title := ""
		if bl.MovieID != nil {
			if m, err := a.svc.store.GetMovie(r.Context(), *bl.MovieID); err == nil {
				title = m.Title
			}
		} else if bl.SeriesID != nil {
			if se, err := a.svc.store.GetSeries(r.Context(), *bl.SeriesID); err == nil {
				title = se.Title
			}
		}
		out = append(out, blocklistDTO{Blocklist: bl, Title: title})
	}
	api.WriteJSON(w, http.StatusOK, out)
}

func (a *API) removeBlocklist(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := a.svc.store.RemoveBlocklist(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "blocklist entry not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to remove blocklist entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) getNaming(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.NamingConfig(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load naming config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) putNaming(w http.ResponseWriter, r *http.Request) {
	var c naming.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetNamingConfig(r.Context(), c); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to save naming config")
		return
	}
	api.WriteJSON(w, http.StatusOK, c)
}

func (a *API) writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRejected):
		api.WriteError(w, http.StatusBadRequest, "rejected", err.Error())
	case errors.Is(err, ErrNoProfile):
		api.WriteError(w, http.StatusBadRequest, "no_profile", err.Error())
	case errors.Is(err, store.ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", "not found")
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
