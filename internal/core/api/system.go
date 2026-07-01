package api

import (
	"log/slog"
	"net/http"
)

type statusResponse struct {
	Version   string `json:"version"`
	AppName   string `json:"appName"`
	Healthy   bool   `json:"healthy"`
	TaskCount int    `json:"taskCount"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.deps.Store.ListTasks(r.Context(), 100)
	if err != nil {
		slog.Default().Error("status failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, statusResponse{
		Version:   s.deps.Version,
		AppName:   "Nexus",
		Healthy:   true,
		TaskCount: len(tasks),
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
