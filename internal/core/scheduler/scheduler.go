package scheduler

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
)

// ErrNoSuchTask is returned by RunNow when no scheduled task matches the
// given name. Any other error from RunNow indicates the task was found but
// enqueueing it failed.
var ErrNoSuchTask = errors.New("scheduler: no such task")

type entry struct {
	name     string
	interval time.Duration
	factory  func() command.Command
	nextRun  time.Time
}

// ScheduledTask is a read-only snapshot of a registered recurring task.
type ScheduledTask struct {
	Name     string
	Interval time.Duration
	NextRun  time.Time
}

type Scheduler struct {
	mgr      *command.Manager
	mu       sync.Mutex
	entries  []*entry
	stop     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func New(m *command.Manager) *Scheduler {
	return &Scheduler{mgr: m, stop: make(chan struct{})}
}

// Every registers a recurring command produced by factory at each interval.
// The task's name is captured once via factory().Name().
func (s *Scheduler) Every(d time.Duration, factory func() command.Command) {
	e := &entry{name: factory().Name(), interval: d, factory: factory}
	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
}

func (s *Scheduler) Start() {
	now := time.Now()
	s.mu.Lock()
	for _, e := range s.entries {
		e.nextRun = now.Add(e.interval)
	}
	s.mu.Unlock()

	for _, e := range s.entries {
		s.wg.Add(1)
		e := e
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(e.interval)
			defer ticker.Stop()
			for {
				select {
				case <-s.stop:
					return
				case <-ticker.C:
					_, _ = s.mgr.Enqueue(e.factory())
					s.mu.Lock()
					e.nextRun = time.Now().Add(e.interval)
					s.mu.Unlock()
				}
			}
		}()
	}
}

// Scheduled returns a snapshot of the registered tasks.
func (s *Scheduler) Scheduled() []ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduledTask, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, ScheduledTask{Name: e.name, Interval: e.interval, NextRun: e.nextRun})
	}
	return out
}

// RunNow enqueues the named task immediately and returns its task id.
func (s *Scheduler) RunNow(name string) (string, error) {
	var factory func() command.Command
	found := false

	s.mu.Lock()
	for _, e := range s.entries {
		if e.name == name {
			factory = e.factory
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return "", fmt.Errorf("%w: %q", ErrNoSuchTask, name)
	}
	return s.mgr.Enqueue(factory())
}

// Stop is safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}
