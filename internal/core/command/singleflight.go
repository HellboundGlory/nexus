package command

import (
	"context"
	"sync/atomic"
)

// SingleFlight wraps a Command so a tick that fires while the previous run is
// still in flight is skipped rather than run concurrently. The Manager has no
// dedupe and several workers, so a scheduled command whose run outlasts its
// interval would otherwise overlap itself.
type SingleFlight struct {
	cmd     Command
	running atomic.Bool
}

// Single returns c guarded against overlapping runs. The returned command is
// stateful: schedule one instance, don't build a fresh one per tick.
func Single(c Command) *SingleFlight { return &SingleFlight{cmd: c} }

func (s *SingleFlight) Name() string { return s.cmd.Name() }

// Run executes the wrapped command unless it is already running, in which case
// it reports success without doing anything — a skipped tick is not an error.
func (s *SingleFlight) Run(ctx context.Context, r Reporter) error {
	if !s.running.CompareAndSwap(false, true) {
		return nil
	}
	defer s.running.Store(false)
	return s.cmd.Run(ctx, r)
}
