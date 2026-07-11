package downloadclient

import (
	"encoding/json"
	"errors"
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
}

func NewAPI(st *store.Store, svc *Service) *API {
	return &API{store: st, svc: svc}
}

// Mount registers routes on an already-authenticated router (the /api/v1 group).
func (a *API) Mount(r chi.Router) {
	r.Route("/downloadclient", func(r chi.Router) {
		r.Get("/", a.list)
		r.Post("/", a.create)
		r.Get("/schema", a.schema)
		r.Post("/test", a.testUnsaved)
		r.Get("/{id}", a.get)
		r.Put("/{id}", a.update)
		r.Delete("/{id}", a.delete)
		r.Post("/{id}/test", a.testSaved)
	})
	r.Post("/download", a.grab)
	r.Get("/queue", a.queue)
	r.Delete("/queue/{clientId}/{itemId}", a.removeItem)
}

type clientPayload struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseSSL         bool   `json:"useSsl"`
	URLBase        string `json:"urlBase"`
	Username       string `json:"username"`
	APIKey         string `json:"apiKey"`
	Category       string `json:"category"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
}

func protocolFor(impl string) string {
	switch impl {
	case "sabnzbd":
		return "usenet"
	case "qbittorrent":
		return "torrent"
	default:
		return ""
	}
}

func (p clientPayload) valid() (string, bool) {
	if strings.TrimSpace(p.Name) == "" {
		return "name is required", false
	}
	if protocolFor(p.Implementation) == "" {
		return "implementation must be sabnzbd or qbittorrent", false
	}
	if strings.TrimSpace(p.Host) == "" {
		return "host is required", false
	}
	return "", true
}

func (p clientPayload) toStore() store.DownloadClient {
	pri := p.Priority
	if pri == 0 {
		pri = 25
	}
	return store.DownloadClient{
		Name: p.Name, Implementation: p.Implementation, Protocol: protocolFor(p.Implementation),
		Host: p.Host, Port: p.Port, UseSSL: p.UseSSL, URLBase: p.URLBase,
		Username: p.Username, APIKey: p.APIKey, Category: p.Category,
		Enabled: p.Enabled, Priority: pri,
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListDownloadClients(r.Context(), false)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to list download clients")
		return
	}
	if rows == nil {
		rows = []store.DownloadClient{}
	}
	api.WriteJSON(w, http.StatusOK, rows)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	id, err := a.store.CreateDownloadClient(r.Context(), p.toStore())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to create download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	dc, _ := a.store.GetDownloadClient(r.Context(), id)
	api.WriteJSON(w, http.StatusCreated, dc)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	dc, err := a.store.GetDownloadClient(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "download client not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load download client")
		return
	}
	api.WriteJSON(w, http.StatusOK, dc)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	dc := p.toStore()
	dc.ID = id
	// Secrets are write-only (APIKey is json:"-"). Empty incoming key means "keep
	// the stored one" so an edit that doesn't re-enter the key can't wipe it.
	// Update-only.
	if p.APIKey == "" {
		existing, err := a.store.GetDownloadClient(r.Context(), id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load download client")
			return
		}
		if err == nil {
			dc.APIKey = existing.APIKey
		}
	}
	if err := a.store.UpdateDownloadClient(r.Context(), dc); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to update download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	updated, _ := a.store.GetDownloadClient(r.Context(), id)
	api.WriteJSON(w, http.StatusOK, updated)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteDownloadClient(r.Context(), id); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to delete download client")
		return
	}
	_ = a.svc.Reload(r.Context())
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) testSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r, "id")
	if !ok {
		return
	}
	dc, err := a.store.GetDownloadClient(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, "not_found", "download client not found")
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", "failed to load download client")
		return
	}
	// Single test run: persist the outcome, then report it.
	terr := a.testClient(r, *dc)
	status, msg := "ok", ""
	if terr != nil {
		status, msg = "failed", terr.Error()
	}
	_ = a.store.SetDownloadClientStatus(r.Context(), id, status, msg)
	if terr != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": terr.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) testUnsaved(w http.ResponseWriter, r *http.Request) {
	var p clientPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if msg, ok := p.valid(); !ok {
		api.WriteError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	a.runTest(w, r, p.toStore())
}

// testClient builds a transient client from a config and runs its Test().
func (a *API) testClient(r *http.Request, dc store.DownloadClient) error {
	base := buildBase(dc)
	var c provider.DownloadClient
	switch dc.Implementation {
	case "sabnzbd":
		c = newSABnzbd("test", base, dc.APIKey, dc.Category, a.svc.http)
	case "qbittorrent":
		c = newQBittorrent("test", base, dc.Username, dc.APIKey, dc.Category, a.svc.http)
	default:
		return ErrUnsupportedProtocol
	}
	return c.Test(r.Context())
}

func (a *API) runTest(w http.ResponseWriter, r *http.Request, dc store.DownloadClient) {
	if err := a.testClient(r, dc); err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) schema(w http.ResponseWriter, r *http.Request) {
	fields := func(credLabel string) []map[string]any {
		return []map[string]any{
			{"name": "name", "type": "string", "required": true},
			{"name": "host", "type": "string", "required": true},
			{"name": "port", "type": "int", "required": false},
			{"name": "useSsl", "type": "bool", "required": false, "default": false},
			{"name": "urlBase", "type": "string", "required": false},
			{"name": "username", "type": "string", "required": false},
			{"name": "apiKey", "type": "string", "required": false, "label": credLabel},
			{"name": "category", "type": "string", "required": false},
			{"name": "priority", "type": "int", "required": false, "default": 25},
			{"name": "enabled", "type": "bool", "required": false, "default": true},
		}
	}
	api.WriteJSON(w, http.StatusOK, []map[string]any{
		{"implementation": "sabnzbd", "protocol": "usenet", "fields": fields("API Key")},
		{"implementation": "qbittorrent", "protocol": "torrent", "fields": fields("Password")},
	})
}

type grabPayload struct {
	DownloadURL string `json:"downloadUrl"`
	Title       string `json:"title"`
	Protocol    string `json:"protocol"`
	IndexerID   string `json:"indexerId"`
	Category    string `json:"category"`
	ClientID    string `json:"clientId"`
}

func (a *API) grab(w http.ResponseWriter, r *http.Request) {
	var p grabPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if strings.TrimSpace(p.DownloadURL) == "" {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "downloadUrl is required")
		return
	}
	req := provider.DownloadRequest{
		URL: p.DownloadURL, Title: p.Title, Protocol: provider.Protocol(p.Protocol),
		IndexerID: p.IndexerID, Category: p.Category,
	}
	id, err := a.svc.Grab(r.Context(), req, p.ClientID)
	if err != nil {
		writeGrabError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"id": id})
}

func writeGrabError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnsupportedProtocol), errors.Is(err, ErrClientUnavailable), errors.Is(err, ErrReleaseUnavailable):
		api.WriteError(w, http.StatusBadRequest, "grab_failed", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func (a *API) queue(w http.ResponseWriter, r *http.Request) {
	res := a.svc.Queue(r.Context())
	if res.Items == nil {
		res.Items = []provider.DownloadItem{}
	}
	if res.ClientErrors == nil {
		res.ClientErrors = []ClientError{}
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (a *API) removeItem(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	itemID := chi.URLParam(r, "itemId")
	deleteData := r.URL.Query().Get("deleteData") == "true"
	if err := a.svc.Remove(r.Context(), clientID, itemID, deleteData); err != nil {
		api.WriteError(w, http.StatusBadRequest, "remove_failed", err.Error())
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func parseID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}
