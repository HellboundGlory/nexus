package media

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
	svc   *Service
}

func NewAPI(st *store.Store, svc *Service) *API { return &API{store: st, svc: svc} }

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Get("/media/lookup", a.lookup)

	r.Route("/series", func(r chi.Router) {
		r.Get("/", a.listSeries)
		r.Post("/", a.addSeries)
		r.Get("/{id}", a.getSeries)
		r.Delete("/{id}", a.deleteSeries)
		r.Post("/{id}/refresh", a.refreshSeries)
		r.Put("/{id}/monitor", a.monitorSeries)
		r.Put("/{id}/qualityprofile", a.assignSeriesProfile)
	})
	r.Put("/season/{id}/monitor", a.monitorSeason)
	r.Put("/episode/{id}/monitor", a.monitorEpisode)

	r.Route("/movies", func(r chi.Router) {
		r.Get("/", a.listMovies)
		r.Post("/", a.addMovie)
		r.Get("/{id}", a.getMovie)
		r.Delete("/{id}", a.deleteMovie)
		r.Post("/{id}/refresh", a.refreshMovie)
		r.Put("/{id}/monitor", a.monitorMovie)
		r.Put("/{id}/qualityprofile", a.assignMovieProfile)
	})

	r.Route("/rootfolder", func(r chi.Router) {
		r.Get("/", a.listRootFolders)
		r.Post("/", a.addRootFolder)
		r.Delete("/{id}", a.deleteRootFolder)
	})
}

func mediaID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

// writeMediaError maps typed media errors to HTTP responses.
func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrAlreadyExists):
		api.WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrInvalidRootFolder):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, ErrProviderNotConfigured):
		api.WriteError(w, http.StatusBadRequest, "not_configured", err.Error())
	case errors.Is(err, ErrProviderUnavailable):
		api.WriteError(w, http.StatusBadGateway, "provider_unavailable", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (a *API) lookup(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	kind := r.URL.Query().Get("kind")
	if term == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "term is required")
		return
	}
	var (
		res []provider.MetadataResult
		err error
	)
	switch kind {
	case "movie":
		res, err = a.svc.meta.SearchMovie(r.Context(), term)
	default:
		res, err = a.svc.meta.SearchTV(r.Context(), term)
	}
	if err != nil {
		writeMediaError(w, err)
		return
	}
	if res == nil {
		res = []provider.MetadataResult{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

type addSeriesBody struct {
	TMDBID        int    `json:"tmdbId"`
	RootFolderID  *int64 `json:"rootFolderId"`
	MonitorOption string `json:"monitorOption"`
}

func (a *API) addSeries(w http.ResponseWriter, r *http.Request) {
	var b addSeriesBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.TMDBID == 0 {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	if b.MonitorOption == "" {
		b.MonitorOption = MonitorAll
	}
	se, err := a.svc.AddSeries(r.Context(), AddSeriesRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, MonitorOption: b.MonitorOption})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, se)
}

type seriesListItem struct {
	store.Series
	EpisodeCount     int `json:"episodeCount"`
	EpisodeFileCount int `json:"episodeFileCount"`
}

func (a *API) listSeries(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListSeries(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list series")
		return
	}
	stats, err := a.store.SeriesEpisodeStats(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list series")
		return
	}
	out := make([]seriesListItem, 0, len(rows))
	for _, s := range rows {
		st := stats[s.ID]
		out = append(out, seriesListItem{Series: s, EpisodeCount: st.EpisodeCount, EpisodeFileCount: st.EpisodeFileCount})
	}
	api.WriteJSON(w, http.StatusOK, out)
}

type episodeDTO struct {
	store.Episode
	HasFile bool `json:"hasFile"`
}

type seriesDetail struct {
	store.Series
	EpisodeCount     int            `json:"episodeCount"`
	EpisodeFileCount int            `json:"episodeFileCount"`
	Seasons          []store.Season `json:"seasons"`
	Episodes         []episodeDTO   `json:"episodes"`
}

func (a *API) getSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	se, err := a.store.GetSeries(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load series")
		return
	}
	seasons, _ := a.store.ListSeasons(r.Context(), id)
	episodes, _ := a.store.ListEpisodes(r.Context(), id)
	if seasons == nil {
		seasons = []store.Season{}
	}
	if episodes == nil {
		episodes = []store.Episode{}
	}
	epFiles, err := a.store.EpisodeFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load series")
		return
	}
	// Enrich with the same monitored-only progress counts the list view shows
	// (see seriesListItem/SeriesEpisodeStats) so the detail header badge matches.
	epDTOs := make([]episodeDTO, 0, len(episodes))
	var monitoredCount, monitoredWithFile int
	for _, e := range episodes {
		hasFile := epFiles[e.ID]
		epDTOs = append(epDTOs, episodeDTO{Episode: e, HasFile: hasFile})
		if e.Monitored {
			monitoredCount++
			if hasFile {
				monitoredWithFile++
			}
		}
	}
	api.WriteJSON(w, http.StatusOK, seriesDetail{
		Series:           *se,
		EpisodeCount:     monitoredCount,
		EpisodeFileCount: monitoredWithFile,
		Seasons:          seasons,
		Episodes:         epDTOs,
	})
}

func (a *API) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteSeries(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete series")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) refreshSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.svc.RefreshSeries(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "series not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type monitorBody struct {
	Monitored bool `json:"monitored"`
}

func decodeMonitor(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var b monitorBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return false, false
	}
	return b.Monitored, true
}

func (a *API) monitorSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	if err := a.svc.SetSeriesMonitored(r.Context(), id, mon); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "series not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	// Resolve the owning series + season number from the season row.
	sn, err := a.store.GetSeason(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "season not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load season")
		return
	}
	if err := a.svc.SetSeasonMonitored(r.Context(), sn.SeriesID, sn.ID, sn.SeasonNumber, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	ep, err := a.store.GetEpisode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "episode not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load episode")
		return
	}
	if err := a.svc.SetEpisodeMonitored(r.Context(), ep.SeriesID, ep.ID, mon); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type addMovieBody struct {
	TMDBID       int    `json:"tmdbId"`
	RootFolderID *int64 `json:"rootFolderId"`
	Monitored    bool   `json:"monitored"`
}

func (a *API) addMovie(w http.ResponseWriter, r *http.Request) {
	var b addMovieBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if b.TMDBID == 0 {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	m, err := a.svc.AddMovie(r.Context(), AddMovieRequest{TMDBID: b.TMDBID, RootFolderID: b.RootFolderID, Monitored: b.Monitored})
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, m)
}

type movieDTO struct {
	store.Movie
	HasFile bool `json:"hasFile"`
}

func (a *API) listMovies(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListMovies(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list movies")
		return
	}
	files, err := a.store.MovieFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list movies")
		return
	}
	out := make([]movieDTO, 0, len(rows))
	for _, m := range rows {
		out = append(out, movieDTO{Movie: m, HasFile: files[m.ID]})
	}
	api.WriteJSON(w, http.StatusOK, out)
}

func (a *API) getMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	m, err := a.store.GetMovie(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load movie")
		return
	}
	files, err := a.store.MovieFileIDs(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, movieDTO{Movie: *m, HasFile: files[m.ID]})
}

func (a *API) deleteMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteMovie(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete movie")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) refreshMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.svc.RefreshMovie(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "movie not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) monitorMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	mon, ok := decodeMonitor(w, r)
	if !ok {
		return
	}
	if err := a.svc.SetMovieMonitored(r.Context(), id, mon); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "movie not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to set monitored")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type assignProfileBody struct {
	QualityProfileID int64 `json:"qualityProfileId"`
}

func (a *API) assignSeriesProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b assignProfileBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetSeriesQualityProfile(r.Context(), id, b.QualityProfileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "series or profile not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) assignMovieProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var b assignProfileBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := a.svc.SetMovieQualityProfile(r.Context(), id, b.QualityProfileID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusNotFound, "not_found", "movie or profile not found")
			return
		}
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type rootFolderBody struct {
	Path string `json:"path"`
}

func (a *API) addRootFolder(w http.ResponseWriter, r *http.Request) {
	var b rootFolderBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	rf, err := a.svc.AddRootFolder(r.Context(), b.Path)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, rf)
}

func (a *API) listRootFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListRootFolders(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list root folders")
		return
	}
	if rows == nil {
		rows = []store.RootFolder{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) deleteRootFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteRootFolder(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete root folder")
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
