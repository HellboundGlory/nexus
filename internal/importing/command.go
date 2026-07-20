package importing

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
)

// ImportCompleted imports every grabbed queue row whose client item is
// completed, and handles rows whose client item has failed (blocklist + retry).
// After a TV import actually lands — the queue row is gone, because ImportItem
// deletes it on success — the series is re-searched, so the next episode is
// grabbed as soon as the slot frees rather than waiting for the next scheduled
// sweep. A SOFT import failure (e.g. no video files found, incomplete import)
// routes through fail(), which sets the row to QueueFailed and retains it
// instead of deleting it, so it does NOT trigger a re-search: the release was
// never blocklisted and the episode is still missing, so an immediate
// re-search could just re-grab the same failing release. Series ids are
// collected and researched once per tick — firing per row would launch
// several concurrent searches racing for the same freed slot.
func (s *Service) ImportCompleted(ctx context.Context) error {
	rows, err := s.store.QueueByStatus(ctx, store.QueueGrabbed)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	items := s.queue.Queue(ctx).Items
	var imported []int64
	seen := map[int64]struct{}{}
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
			// A successful import deletes the queue row; a soft failure
			// (fail()) retains it as QueueFailed. Gate the re-search on the
			// row actually being gone rather than on err == nil, since
			// fail() itself returns nil.
			if _, gerr := s.store.GetQueueItem(ctx, row.ID); errors.Is(gerr, store.ErrNotFound) {
				if row.SeriesID != nil {
					if _, dup := seen[*row.SeriesID]; !dup {
						seen[*row.SeriesID] = struct{}{}
						imported = append(imported, *row.SeriesID)
					}
				}
			}
		case provider.StatusFailed:
			if err := s.handleFailed(ctx, row, it); err != nil {
				return err
			}
		}
	}
	s.researchImportedSeries(ctx, imported)
	return nil
}

// researchImportedSeries re-searches each series that just had an import land,
// so the freed concurrency slot is used immediately. Failures are logged and
// swallowed: an import must never fail because a follow-up search did.
func (s *Service) researchImportedSeries(ctx context.Context, seriesIDs []int64) {
	if s.researcher == nil {
		return
	}
	for _, id := range seriesIDs {
		if err := s.researcher.ResearchSeries(ctx, id); err != nil {
			slog.Warn("importing: re-search after import failed", "seriesId", id, "err", err)
		}
	}
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
// TV rows re-search the whole SERIES rather than each episode: for a season pack
// the per-episode variant skipped straight to per-episode grabbing, so the
// next-best pack was never tried. Re-running the series search re-enters the
// season-pack branch with the failed pack blocklisted, which is what makes pack
// exhaustion work.
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
	if row.SeriesID != nil {
		if err := s.researcher.ResearchSeries(ctx, *row.SeriesID); err != nil {
			slog.Warn("importing: re-search after failure failed", "seriesId", *row.SeriesID, "err", err)
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
