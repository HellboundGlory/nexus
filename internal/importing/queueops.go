package importing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hellboundg/nexus/internal/core/store"
)

// ErrClientUnavailable reports that a download client could not be reached, so
// an operation that must not act on an incomplete picture was refused.
var ErrClientUnavailable = errors.New("download client unavailable")

// RemoveOptions controls what happens alongside deleting a queue row.
type RemoveOptions struct {
	// RemoveFromClient cancels the download in its download client and deletes
	// its data. When the client call fails the row is KEPT, so the user can
	// retry; clearing this flag deletes the row unconditionally.
	RemoveFromClient bool
	// Blocklist records the release so automation will not grab it again.
	// Without it the next missing-search sweep may re-grab the same file.
	Blocklist bool
}

// RemoveQueueItem deletes one queue row, optionally cancelling its download and
// blocklisting the release first.
//
// When RemoveFromClient is true, a missing live match is read three ways:
//   - No match, and every client answered (ClientErrors is empty): the client
//     has already finished with this download, so there is nothing to cancel.
//     The row is deleted normally.
//   - No match, but ClientErrors is non-empty: the snapshot is partial, so an
//     unreachable client is indistinguishable from a download it still has.
//     Refuse rather than risk orphaning it — the row is KEPT. This mirrors
//     ClearQueue's preflight (§4.4).
//   - A live match whose Remove call fails: the download is still running and
//     the client told us so. Refuse and keep the row, same as above.
func (s *Service) RemoveQueueItem(ctx context.Context, id int64, opts RemoveOptions) error {
	row, err := s.store.GetQueueItem(ctx, id)
	if err != nil {
		return err // store.ErrNotFound surfaces as 404
	}
	if opts.RemoveFromClient {
		snap := s.queue.Queue(ctx)
		if it, ok := matchItem(snap.Items, row); ok {
			if err := s.queue.Remove(ctx, it.DownloadClientID, it.ID, true); err != nil {
				// Keep the row: the download is still running, and deleting the
				// row now would orphan it with nothing tracking it.
				return fmt.Errorf("%w: %s", ErrClientUnavailable, err)
			}
		} else if len(snap.ClientErrors) > 0 {
			// No live match, but the snapshot is incomplete: an unreachable
			// client contributes zero items, so it looks identical to "the
			// client has already finished with this download." We cannot tell
			// them apart, so refuse rather than risk orphaning a download that
			// is still running on the client we couldn't reach.
			return fmt.Errorf("%w: %s", ErrClientUnavailable, describeClientErrors(snap.ClientErrors))
		}
		// No live match and every client answered: the client has already
		// finished with this download, so there is nothing to cancel. Deleting
		// the row is correct.
	}
	if opts.Blocklist {
		if _, err := s.store.AddBlocklist(ctx, store.Blocklist{
			MediaKind: row.MediaKind, MovieID: row.MovieID, SeriesID: row.SeriesID,
			SourceTitle: row.SourceTitle, Protocol: row.Protocol,
			QualityID: row.QualityID, Reason: "removed from queue",
		}); err != nil {
			// Abort before deleting the row so a retry loses nothing.
			return err
		}
	}
	if err := s.store.DeleteQueueItem(ctx, id); err != nil {
		return err
	}
	s.emit(ctx, QueueUpdated{ID: id})
	return nil
}

// ClearResult reports what a ClearQueue call actually did. ClientErrors is
// non-empty only for a forced clear that tolerated failures.
type ClearResult struct {
	Removed      int           `json:"removed"`
	ClientErrors []ClientError `json:"clientErrors,omitempty"`
}

// ClearQueue removes every queue row, cancelling each download in its client.
//
// When force is false an unreachable client refuses the whole operation before
// anything is deleted — clearing against an incomplete picture would orphan
// downloads Nexus can no longer see.
//
// force does NOT skip the client removals; it only tolerates their failure, so
// a client that is merely flaky still gets its downloads cancelled properly.
func (s *Service) ClearQueue(ctx context.Context, force bool) (ClearResult, error) {
	snap := s.queue.Queue(ctx)
	var res ClearResult
	if len(snap.ClientErrors) > 0 {
		if !force {
			return ClearResult{}, fmt.Errorf("%w: %s", ErrClientUnavailable, describeClientErrors(snap.ClientErrors))
		}
		res.ClientErrors = append(res.ClientErrors, snap.ClientErrors...)
	}
	rows, err := s.store.ListQueue(ctx)
	if err != nil {
		return res, err
	}
	for _, row := range rows {
		if it, ok := matchItem(snap.Items, row); ok {
			if err := s.queue.Remove(ctx, it.DownloadClientID, it.ID, true); err != nil {
				if !force {
					return res, fmt.Errorf("%w: %s", ErrClientUnavailable, err)
				}
				slog.Warn("importing: clear queue could not remove download from client",
					"queueId", row.ID, "clientId", it.DownloadClientID, "err", err)
				res.ClientErrors = append(res.ClientErrors, ClientError{
					ClientID: it.DownloadClientID, Message: err.Error(),
				})
			}
		}
		if err := s.store.DeleteQueueItem(ctx, row.ID); err != nil {
			return res, err
		}
		res.Removed++
		s.emit(ctx, QueueUpdated{ID: row.ID})
	}
	return res, nil
}

func describeClientErrors(errs []ClientError) string {
	if len(errs) == 1 {
		return fmt.Sprintf("%s: %s", errs[0].ClientID, errs[0].Message)
	}
	return fmt.Sprintf("%d download clients could not be reached", len(errs))
}
