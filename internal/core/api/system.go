package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type statusResponse struct {
	Version   string `json:"version"`
	AppName   string `json:"appName"`
	Healthy   bool   `json:"healthy"`
	TaskCount int    `json:"taskCount"`
}

type scheduledDTO struct {
	Name                string     `json:"name"`
	IntervalSeconds     int        `json:"intervalSeconds"`
	LastExecution       *time.Time `json:"lastExecution"`
	LastDurationSeconds *int       `json:"lastDurationSeconds"`
	NextExecution       time.Time  `json:"nextExecution"`
}

type queueDTO struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	QueuedAt        time.Time  `json:"queuedAt"`
	StartedAt       *time.Time `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	DurationSeconds *int       `json:"durationSeconds"`
}

type tasksResponse struct {
	Scheduled []scheduledDTO `json:"scheduled"`
	Queue     []queueDTO     `json:"queue"`
}

func durationSeconds(start, end *time.Time) *int {
	if start == nil || end == nil {
		return nil
	}
	d := int(end.Sub(*start).Seconds())
	if d < 0 {
		d = 0
	}
	return &d
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

func (s *server) handleTasks(w http.ResponseWriter, r *http.Request) {
	sched := s.deps.Tasks.Scheduled()
	out := tasksResponse{Scheduled: make([]scheduledDTO, 0, len(sched))}
	for _, t := range sched {
		dto := scheduledDTO{
			Name:            t.Name,
			IntervalSeconds: int(t.Interval.Seconds()),
			NextExecution:   t.NextRun,
		}
		last, err := s.deps.Store.LastTaskByName(r.Context(), t.Name)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if last != nil {
			when := last.CreatedAt
			if last.EndedAt != nil {
				when = *last.EndedAt
			}
			dto.LastExecution = &when
			dto.LastDurationSeconds = durationSeconds(last.StartedAt, last.EndedAt)
		}
		out.Scheduled = append(out.Scheduled, dto)
	}

	rows, err := s.deps.Store.ListTasks(r.Context(), 50)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	out.Queue = make([]queueDTO, 0, len(rows))
	for _, t := range rows {
		out.Queue = append(out.Queue, queueDTO{
			ID: t.ID, Name: t.Name, Status: t.Status,
			QueuedAt: t.CreatedAt, StartedAt: t.StartedAt, EndedAt: t.EndedAt,
			DurationSeconds: durationSeconds(t.StartedAt, t.EndedAt),
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

func (s *server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id, err := s.deps.Tasks.RunNow(name)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "no such scheduled task")
		return
	}
	WriteJSON(w, http.StatusAccepted, map[string]string{"taskId": id})
}
