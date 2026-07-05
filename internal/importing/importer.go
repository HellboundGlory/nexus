package importing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/naming"
	"github.com/hellboundg/nexus/internal/parsing"
	"github.com/hellboundg/nexus/internal/quality"
)

// ImportItem imports the completed download tracked by the given queue row.
func (s *Service) ImportItem(ctx context.Context, queueID int64) error {
	row, err := s.store.GetQueueItem(ctx, queueID)
	if err != nil {
		return err
	}
	outputPath, ok := s.completedOutputPath(ctx, row)
	if !ok {
		return s.fail(ctx, row, "download not completed or not found")
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImporting, "")

	cfg, err := s.NamingConfig(ctx)
	if err != nil {
		return s.fail(ctx, row, "load naming config: "+err.Error())
	}
	files, err := videoFilesIn(outputPath)
	if err != nil {
		return s.fail(ctx, row, "scan output: "+err.Error())
	}
	if len(files) == 0 {
		return s.fail(ctx, row, "no video files found")
	}

	kind := provider.MediaKind(row.MediaKind)
	imported := 0
	for _, f := range files {
		if err := s.importFile(ctx, row, kind, cfg, f); err != nil {
			return s.fail(ctx, row, err.Error())
		}
		imported++
	}
	if imported == 0 {
		return s.fail(ctx, row, "nothing imported")
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImported, "")
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueImported})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
}

// completedOutputPath finds the client item for this row and returns its output
// path if it is completed.
func (s *Service) completedOutputPath(ctx context.Context, row store.QueueItem) (string, bool) {
	for _, it := range s.queue.Queue(ctx) {
		if it.DownloadClientID == row.DownloadClientID && it.ID == row.ClientItemID {
			if it.Status == provider.StatusCompleted && it.OutputPath != "" {
				return it.OutputPath, true
			}
			return "", false
		}
	}
	return "", false
}

// importFile places one video file for a movie or single episode (first import).
func (s *Service) importFile(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, srcFile string) error {
	parsed := parsing.Parse(filepath.Base(srcFile), kind)
	q := quality.Resolve(parsed)
	if q.ID == 0 {
		if d, ok := quality.DefinitionByID(row.QualityID); ok {
			q = d
		}
	}
	ext := filepath.Ext(srcFile)

	if kind == provider.KindMovie {
		m, err := s.store.GetMovie(ctx, *row.MovieID)
		if err != nil {
			return err
		}
		root, err := s.rootPath(ctx, m.RootFolderID)
		if err != nil {
			return err
		}
		tok := naming.Tokens{MovieTitle: m.Title, Year: m.Year, Quality: q.Name, ReleaseGroup: parsed.ReleaseGroup}
		dst := filepath.Join(root, naming.Sanitize(naming.Render(cfg.MovieFolder, tok)), naming.Sanitize(naming.Render(cfg.MovieFile, tok))+ext)
		return s.placeAndRecord(ctx, row, dst, root, srcFile, q, store.MediaFile{MediaKind: "movie", MovieID: row.MovieID})
	}

	// TV: single episode = the one recorded episode id.
	if len(row.EpisodeIDs) != 1 {
		return fmt.Errorf("season-pack import not handled in this task")
	}
	epID := row.EpisodeIDs[0]
	ep, err := s.store.GetEpisode(ctx, epID)
	if err != nil {
		return err
	}
	se, err := s.store.GetSeries(ctx, *row.SeriesID)
	if err != nil {
		return err
	}
	root, err := s.rootPath(ctx, se.RootFolderID)
	if err != nil {
		return err
	}
	tok := naming.Tokens{
		SeriesTitle: se.Title, EpisodeTitle: ep.Title, Quality: q.Name,
		ReleaseGroup: parsed.ReleaseGroup, Season: ep.SeasonNumber, Episode: ep.EpisodeNumber,
	}
	dst := filepath.Join(root,
		naming.Sanitize(naming.Render(cfg.SeriesFolder, tok)),
		naming.Sanitize(naming.Render(cfg.SeasonFolder, tok)),
		naming.Sanitize(naming.Render(cfg.EpisodeFile, tok))+ext)
	return s.placeAndRecord(ctx, row, dst, root, srcFile, q, store.MediaFile{MediaKind: "tv", EpisodeID: &epID})
}

// placeAndRecord hardlinks the file, records the media_files row (relative to the
// root folder), and writes an imported history entry.
func (s *Service) placeAndRecord(ctx context.Context, row store.QueueItem, dst, root, srcFile string, q quality.QualityDefinition, mf store.MediaFile) error {
	if err := placeFile(srcFile, dst); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dst)
	if err != nil {
		rel = dst
	}
	fi, _ := os.Stat(dst)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	mf.RelativePath = filepath.ToSlash(rel)
	mf.Size = size
	mf.QualityID = q.ID
	if _, err := s.store.UpsertMediaFile(ctx, mf); err != nil {
		return err
	}
	qid := q.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "imported", MediaKind: row.MediaKind, SeriesID: row.SeriesID, MovieID: row.MovieID,
		EpisodeID: mf.EpisodeID, SourceTitle: row.SourceTitle, QualityID: &qid,
	})
	return nil
}

func (s *Service) rootPath(ctx context.Context, rootFolderID *int64) (string, error) {
	if rootFolderID == nil {
		return "", fmt.Errorf("item has no root folder")
	}
	rf, err := s.store.GetRootFolder(ctx, *rootFolderID)
	if err != nil {
		return "", err
	}
	return rf.Path, nil
}

// fail marks the row failed, records history, and emits.
func (s *Service) fail(ctx context.Context, row store.QueueItem, msg string) error {
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueFailed, msg)
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: "import_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
		MovieID: row.MovieID, SourceTitle: row.SourceTitle, Message: msg,
	})
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueFailed})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
}
