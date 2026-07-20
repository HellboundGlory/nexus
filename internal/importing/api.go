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
		r.Delete("/", a.clearQueue)
		r.Delete("/{id}", a.deleteQueue)
		r.Post("/{id}/import", a.importItem)
	})
	r.Route("/history", func(r chi.Router) {
		r.Get("/", a.history)
		r.Delete("/", a.clearHistory)
	})
	r.Route("/blocklist", func(r chi.Router) {
		r.Get("/", a.listBlocklist)
		r.Delete("/", a.clearBlocklist)
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

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// pageParams reads 1-based ?page= and ?pageSize=, clamping both into range, and
// returns the page, the size, and the corresponding SQL offset.
func pageParams(r *http.Request) (page, size, offset int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size, (page - 1) * size
}

// pagedResponse is the envelope for the paged list endpoints. Items is always a
// JSON array, never null.
type pagedResponse struct {
	Items    any `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// boolParam reads a query flag, returning def when absent or unparseable.
func boolParam(r *http.Request, name string, def bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

type queueItemDTO struct {
	store.QueueItem
	Progress       *float64 `json:"progress,omitempty"`       // 0..100, nil when no live match
	DownloadStatus string   `json:"downloadStatus,omitempty"` // provider.DownloadStatus, "" when no live match
}

func (a *API) listQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.store.ListQueue(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list queue")
		return
	}
	live := a.svc.queue.Queue(r.Context()).Items
	out := make([]queueItemDTO, 0, len(rows))
	for _, row := range rows {
		dto := queueItemDTO{QueueItem: row}
		if it, ok := matchItem(live, row); ok {
			p := it.Progress
			dto.Progress = &p
			dto.DownloadStatus = string(it.Status)
		}
		out = append(out, dto)
	}
	api.WriteJSON(w, http.StatusOK, out)
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
	Force       bool               `json:"force"`
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
		ClientID: b.ClientID, MediaKind: b.MediaKind, SeriesID: b.SeriesID, EpisodeIDs: b.EpisodeIDs,
		MovieID: b.MovieID, Force: b.Force,
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
	// removeFromClient defaults to true so a bare DELETE does the safe thing
	// (no orphaned download); unchecking it is the escape hatch when the client
	// is unreachable.
	opts := RemoveOptions{
		RemoveFromClient: boolParam(r, "removeFromClient", true),
		Blocklist:        boolParam(r, "blocklist", false),
	}
	if err := a.svc.RemoveQueueItem(r.Context(), id, opts); err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) clearQueue(w http.ResponseWriter, r *http.Request) {
	res, err := a.svc.ClearQueue(r.Context(), boolParam(r, "force", false))
	if err != nil {
		a.writeErr(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (a *API) clearHistory(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.store.ClearHistory(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to clear history")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]int64{"removed": n})
}

func (a *API) clearBlocklist(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.store.ClearBlocklist(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to clear blocklist")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]int64{"removed": n})
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
	page, size, offset := pageParams(r)
	rows, total, err := a.svc.store.ListHistoryPage(r.Context(), offset, size)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list history")
		return
	}
	api.WriteJSON(w, http.StatusOK, pagedResponse{Items: rows, Page: page, PageSize: size, Total: total})
}

type blocklistDTO struct {
	store.Blocklist
	Title string `json:"title"` // movie/series display title, "" if deleted
}

func (a *API) listBlocklist(w http.ResponseWriter, r *http.Request) {
	page, size, offset := pageParams(r)
	rows, total, err := a.svc.store.ListBlocklistPage(r.Context(), offset, size)
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
	api.WriteJSON(w, http.StatusOK, pagedResponse{Items: out, Page: page, PageSize: size, Total: total})
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
	case errors.Is(err, ErrClientUnavailable):
		api.WriteError(w, http.StatusServiceUnavailable, "client_unavailable", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
