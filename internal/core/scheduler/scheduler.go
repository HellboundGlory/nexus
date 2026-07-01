package scheduler

import (
	"sync"
	"time"

	"github.com/hellboundg/nexus/internal/core/command"
)

type entry struct {
	interval time.Duration
	factory  func() command.Command
}

type Scheduler struct {
	mgr     *command.Manager
	entries []entry
	stop    chan struct{}
	wg      sync.WaitGroup
}

func New(m *command.Manager) *Scheduler {
	return &Scheduler{mgr: m, stop: make(chan struct{})}
}

// Every registers a recurring command produced by factory at each interval.
func (s *Scheduler) Every(d time.Duration, factory func() command.Command) {
	s.entries = append(s.entries, entry{interval: d, factory: factory})
}

func (s *Scheduler) Start() {
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
				}
			}
		}()
	}
}

func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}
