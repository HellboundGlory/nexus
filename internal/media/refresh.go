package media

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/command"
)

// RefreshCommand refreshes all monitored library items on a schedule. A single
// instance is registered with the scheduler (it is stateless, so re-use is fine).
type RefreshCommand struct {
	svc *Service
}

func NewRefresh(svc *Service) *RefreshCommand { return &RefreshCommand{svc: svc} }

func (c *RefreshCommand) Name() string { return "MediaRefresh" }

func (c *RefreshCommand) Run(ctx context.Context, r command.Reporter) error {
	err := c.svc.RefreshAll(ctx)
	r.Progress(100, "")
	return err
}
