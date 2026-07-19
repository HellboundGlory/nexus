package importing

import (
	"context"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ImportCompleted imports every grabbed queue row whose client item is
// completed, and handles rows whose client item has failed (blocklist + retry).
func (s *Service) ImportCompleted(ctx context.Context) error {
	rows, err := s.store.QueueByStatus(ctx, store.QueueGrabbed)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	items := s.queue.Queue(ctx).Items
	for _, row := range rows {
		it, ok := matchItem(items, row)
		if !ok {
			continue
		}
		switch it.Status {
		case provider.StatusCompleted:
			if err := s.ImportItem(ctx, row.ID); err != nil {
				return err
			}
		case provider.StatusFailed:
			if err := s.handleFailed(ctx, row, it); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleFailed records the failure, blocklists the release (scoped to the media
// item), removes the dead client item, deletes the queue row, and re-searches.
func (s *Service) handleFailed(ctx context.Context, row store.QueueItem, it provider.DownloadItem) error {
	reason := it.ErrorMessage
	qid := row.QualityID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "download_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
		MovieID: row.MovieID, SourceTitle: row.SourceTitle, QualityID: &qid, Message: reason,
	})
	if _, err := s.store.AddBlocklist(ctx, store.Blocklist{
		MediaKind: row.MediaKind, MovieID: row.MovieID, SeriesID: row.SeriesID,
		SourceTitle: row.SourceTitle, Protocol: row.Protocol, QualityID: row.QualityID, Reason: reason,
	}); err != nil {
		return err
	}
	if it.DownloadClientID != "" && it.ID != "" {
		_ = s.queue.Remove(ctx, it.DownloadClientID, it.ID, true)
	}
	if err := s.store.DeleteQueueItem(ctx, row.ID); err != nil {
		return err
	}
	s.emit(ctx, DownloadFailedEvent{
		QueueID: row.ID, MediaKind: row.MediaKind, MovieID: row.MovieID,
		SeriesID: row.SeriesID, EpisodeIDs: row.EpisodeIDs,
	})
	s.researchAfterFailure(ctx, row)
	return nil
}

// researchAfterFailure triggers a fresh search for the failed target so
// automation can grab an alternative release, now that this one is blocklisted.
func (s *Service) researchAfterFailure(ctx context.Context, row store.QueueItem) {
	if s.researcher == nil {
		return
	}
	if row.MediaKind == string(provider.KindMovie) && row.MovieID != nil {
		if err := s.researcher.ResearchMovie(ctx, *row.MovieID); err != nil {
			slog.Warn("importing: re-search after failure failed", "movieId", *row.MovieID, "err", err)
		}
		return
	}
	for _, epID := range row.EpisodeIDs {
		if err := s.researcher.ResearchEpisode(ctx, epID); err != nil {
			slog.Warn("importing: re-search after failure failed", "episodeId", epID, "err", err)
		}
	}
}

// ImportCommand adapts ImportCompleted to the scheduler's command.Command.
type ImportCommand struct{ svc *Service }

func NewImportCommand(svc *Service) *ImportCommand { return &ImportCommand{svc: svc} }

func (c *ImportCommand) Name() string { return "ImportCompletedDownloads" }

func (c *ImportCommand) Run(ctx context.Context, _ command.Reporter) error {
	return c.svc.ImportCompleted(ctx)
}
