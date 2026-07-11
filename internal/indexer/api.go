package indexer

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

type API struct {
	store *store.Store
	svc   *Service
	http  *http.Client
}

func NewAPI(st *store.Store, svc *Service, hc *http.Client) *API {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &API{store: st, svc: svc, http: hc}
}

// Mount registers indexer routes on an already-authenticated router (the
// /api/v1 group). Call via api.NewRouter(..., indexerAPI.Mount).
func (a *API) Mount(r chi.Router) {
	r.Route("/indexer", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Get("/schema", a.schema)
		r.Post("/test", a.testUnsaved)
		r.Get("/{id}", a.get)
		r.Put("/{id}", a.update)
		r.Delete("/{id}", a.delete)
		r.Post("/{id}/test", a.testSaved)
	})
	r.Get("/search", a.search)
}

type indexerPayload struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	Categories     []int  `json:"categories"`
}

func (p indexerPayload) toStore() store.Indexer {
	pri := p.Priority
	if pri == 0 {
		pri = 25
	}
	return store.Indexer{
		Name: p.Name, Implementation: p.Implementation, BaseURL: p.BaseURL,
		APIKey: p.APIKey, Enabled: p.Enabled, Priority: pri, Categories: p.Categories,
	}
}

func (p indexerPayload) valid() (string, bool) {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required", false
	}
	if p.Implementation != "newznab" && p.Implementation != "torznab" {
		return "implementation must be newznab or torznab", false
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return "baseUrl is required", false
	}
	return "", true
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListIndexers(r.Context(), false)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list indexers")
		return
	}
	if rows == nil {
		rows = []store.Indexer{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	id, err := a.store.CreateIndexer(r.Context(), p.toStore())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to create indexer")
		return
	}
	// Best-effort caps discovery; failure is non-fatal (status becomes failed).
	a.refreshOne(r, id, p.BaseURL, p.APIKey)
	_ = a.svc.Reload(r.Context())
	ix, _ := a.store.GetIndexer(r.Context(), id)
	api.WriteJSON(w, http.StatusCreated, ix)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ix, err := a.store.GetIndexer(r.Context(), id)
	if err == store.ErrNotFound {
		api.WriteError(w, http.StatusNotFound, "not_found", "indexer not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load indexer")
		return
	}
	api.WriteJSON(w, http.StatusOK, ix)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	ix := p.toStore()
	ix.ID = id
	// Secrets are write-only (APIKey is json:"-"), so the edit form loads the key
	// blank. An empty incoming key means "keep the stored one" — otherwise every
	// edit-without-retyping would wipe it. Update-only: empty on create is a
	// legitimate keyless indexer.
	if p.APIKey == "" {
		if existing, err := a.store.GetIndexer(r.Context(), id); err == nil {
			ix.APIKey = existing.APIKey
		}
	}
	if err := a.store.UpdateIndexer(r.Context(), ix); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to update indexer")
		return
	}
	_ = a.svc.Reload(r.Context())
	updated, _ := a.store.GetIndexer(r.Context(), id)
	api.WriteJSON(w, http.StatusOK, updated)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteIndexer(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete indexer")
		return
	}
	_ = a.svc.Reload(r.Context())
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) testSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ix, err := a.store.GetIndexer(r.Context(), id)
	if err == store.ErrNotFound {
		api.WriteError(w, http.StatusNotFound, "not_found", "indexer not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load indexer")
		return
	}
	a.writeTestResult(w, r, ix.BaseURL, ix.APIKey)
	a.refreshOne(r, id, ix.BaseURL, ix.APIKey)
	_ = a.svc.Reload(r.Context())
}

func (a *API) testUnsaved(w http.ResponseWriter, r *http.Request) {
	var p indexerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	a.writeTestResult(w, r, p.BaseURL, p.APIKey)
}

func (a *API) writeTestResult(w http.ResponseWriter, r *http.Request, base, apiKey string) {
	caps, err := discoverCaps(r.Context(), a.http, base, apiKey)
	if err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "capabilities": caps})
}

func (a *API) schema(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, []map[string]any{
		{"implementation": "newznab", "protocol": "usenet", "fields": indexerSchemaFields()},
		{"implementation": "torznab", "protocol": "torrent", "fields": indexerSchemaFields()},
	})
}

func indexerSchemaFields() []map[string]any {
	return []map[string]any{
		{"name": "name", "type": "string", "required": true},
		{"name": "baseUrl", "type": "string", "required": true},
		{"name": "apiKey", "type": "string", "required": false},
		{"name": "categories", "type": "int[]", "required": false},
		{"name": "priority", "type": "int", "required": false, "default": 25},
		{"name": "enabled", "type": "bool", "required": false, "default": true},
	}
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := provider.Query{
		Type: provider.SearchType(defaultStr(r.URL.Query().Get("type"), string(provider.SearchGeneric))),
		Term: r.URL.Query().Get("query"),
	}
	for _, c := range strings.Split(r.URL.Query().Get("categories"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			if n, err := strconv.Atoi(c); err == nil {
				q.Categories = append(q.Categories, n)
			}
		}
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		q.Limit = n
	}
	res := a.svc.Search(r.Context(), q)
	// Note: release DownloadURL/InfoURL may embed the indexer's ?apikey= — this
	// is required for the download client to grab the release and is an accepted
	// scope (the whole /api/v1 surface is admin-authed). The indexer *config*
	// API key stays write-only (store.Indexer.APIKey is json:"-"). See design
	// spec §10.1.
	if res.Releases == nil {
		res.Releases = []provider.Release{}
	}
	if res.IndexerErrors == nil {
		res.IndexerErrors = []IndexerError{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

// refreshOne runs caps discovery for one indexer and records the result.
func (a *API) refreshOne(r *http.Request, id int64, base, apiKey string) {
	caps, err := discoverCaps(r.Context(), a.http, base, apiKey)
	if err != nil {
		_ = a.store.SetIndexerStatus(r.Context(), id, "failed", err.Error(), "")
		return
	}
	if b, mErr := json.Marshal(caps); mErr == nil {
		_ = a.store.SetIndexerStatus(r.Context(), id, "ok", "", string(b))
	}
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func defaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
