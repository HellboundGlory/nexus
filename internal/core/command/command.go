package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/store"
)

type Reporter interface{ Progress(pct int, msg string) }

type Command interface {
	Name() string
	Run(ctx context.Context, r Reporter) error
}

// TaskUpdated is emitted on every task state change.
type TaskUpdated struct{ Task store.Task }

func (TaskUpdated) Name() string { return "task.updated" }

type job struct {
	id  string
	cmd Command
}

type Manager struct {
	store   *store.Store
	bus     *events.Bus
	workers int
	queue   chan job
	wg      sync.WaitGroup
	log     *slog.Logger
}

func NewManager(s *store.Store, bus *events.Bus, workers int) *Manager {
	if workers < 1 {
		workers = 1
	}
	return &Manager{
		store:   s,
		bus:     bus,
		workers: workers,
		queue:   make(chan job, 256),
		log:     slog.Default(),
	}
}

func (m *Manager) WithLogger(l *slog.Logger) *Manager { m.log = l; return m }

func (m *Manager) Start() {
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

// Stop closes the queue and waits for in-flight jobs to drain.
func (m *Manager) Stop() {
	close(m.queue)
	m.wg.Wait()
}

func (m *Manager) Enqueue(c Command) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	t := store.Task{ID: id, Name: c.Name(), Status: "queued"}
	if err := m.store.UpsertTask(context.Background(), t); err != nil {
		return "", err
	}
	m.emit(t)
	m.queue <- job{id: id, cmd: c}
	return id, nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for j := range m.queue {
		m.run(j)
	}
}

func (m *Manager) run(j job) {
	ctx := context.Background()
	rep := &reporter{m: m, id: j.id, name: j.cmd.Name()}
	m.update(j.id, j.cmd.Name(), "running", 0, "")

	defer func() {
		if r := recover(); r != nil {
			m.log.Error("command panicked", "id", j.id, "recover", r)
			m.update(j.id, j.cmd.Name(), "failed", rep.pct, "panic")
		}
	}()

	if err := j.cmd.Run(ctx, rep); err != nil {
		m.update(j.id, j.cmd.Name(), "failed", rep.pct, err.Error())
		return
	}
	m.update(j.id, j.cmd.Name(), "completed", 100, "")
}

func (m *Manager) update(id, name, status string, pct int, msg string) {
	t := store.Task{ID: id, Name: name, Status: status, Progress: pct, Message: msg}
	if err := m.store.UpsertTask(context.Background(), t); err != nil {
		m.log.Error("persist task", "id", id, "err", err)
		return
	}
	m.emit(t)
}

func (m *Manager) emit(t store.Task) {
	m.bus.PublishAsync(context.Background(), TaskUpdated{Task: t})
}

type reporter struct {
	m    *Manager
	id   string
	name string
	pct  int
}

func (r *reporter) Progress(pct int, msg string) {
	r.pct = pct
	r.m.update(r.id, r.name, "running", pct, msg)
}

func newID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
