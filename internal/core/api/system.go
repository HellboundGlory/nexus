package api

import (
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
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
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
