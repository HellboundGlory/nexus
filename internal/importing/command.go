package importing

import (
	"context"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ImportCompleted imports every grabbed queue row whose client item is completed.
func (s *Service) ImportCompleted(ctx context.Context) error {
	rows, err := s.store.QueueByStatus(ctx, store.QueueGrabbed)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	completed := map[string]bool{}
	for _, it := range s.queue.Queue(ctx) {
		if it.Status == provider.StatusCompleted {
			completed[it.DownloadClientID+"|"+it.ID] = true
		}
	}
	for _, row := range rows {
		if !completed[row.DownloadClientID+"|"+row.ClientItemID] {
			continue
		}
		if err := s.ImportItem(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// ImportCommand adapts ImportCompleted to the scheduler's command.Command.
type ImportCommand struct{ svc *Service }

func NewImportCommand(svc *Service) *ImportCommand { return &ImportCommand{svc: svc} }

func (c *ImportCommand) Name() string { return "ImportCompletedDownloads" }

func (c *ImportCommand) Run(ctx context.Context, _ command.Reporter) error {
	return c.svc.ImportCompleted(ctx)
}
