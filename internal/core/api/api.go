package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/store"
)

type Deps struct {
	Auth    *auth.Service
	Store   *store.Store
	Version string
}

type server struct{ deps Deps }

func NewRouter(d Deps, spa http.Handler) http.Handler {
	s := &server{deps: d}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Get("/system/status", s.handleStatus)
			// WebSocket route is registered in ws.go via RegisterWebSocket (Task 10).
			s.registerWebSocket(r)
		})
	})

	// SPA fallback for everything else.
	r.NotFound(spa.ServeHTTP)
	return r
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	token, err := s.deps.Auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrUnauthorized) {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.deps.Auth.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Temporary stub for Task 10; replaced with real implementation in ws.go
func (s *server) registerWebSocket(r chi.Router) {}
