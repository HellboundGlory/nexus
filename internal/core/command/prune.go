package command

import (
	"context"
	"fmt"

	"github.com/hellboundg/nexus/internal/core/store"
)

// pruneTasks is the scheduled Housekeeping command: it prunes the tasks table
// to the newest `keep` terminal rows per task name.
type pruneTasks struct {
	store *store.Store
	keep  int
}

// NewPruneTasks returns the Housekeeping command.
func NewPruneTasks(s *store.Store, keep int) Command {
	return &pruneTasks{store: s, keep: keep}
}

func (p *pruneTasks) Name() string { return "Housekeeping" }

func (p *pruneTasks) Run(ctx context.Context, r Reporter) error {
	n, err := p.store.PruneTasksPerName(ctx, p.keep)
	if err != nil {
		return err
	}
	r.Progress(100, fmt.Sprintf("%d pruned", n))
	return nil
}
