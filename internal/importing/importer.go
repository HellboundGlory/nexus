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
	item, ok := matchItem(s.queue.Queue(ctx), row)
	if !ok || item.Status != provider.StatusCompleted || item.OutputPath == "" {
		return s.fail(ctx, row, "download not completed or not found")
	}
	outputPath := item.OutputPath
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
	placed := 0
	for _, f := range files {
		ok, err := s.importFile(ctx, row, kind, cfg, f)
		if err != nil {
			return s.fail(ctx, row, err.Error())
		}
		if ok {
			placed++
		}
	}
	if placed == 0 {
		return s.fail(ctx, row, "no files imported (rejected as non-upgrade or unmatched)")
	}
	if !s.allTargetsHaveFiles(ctx, row, kind) {
		return s.fail(ctx, row, fmt.Sprintf("incomplete import (%d file(s) placed)", placed))
	}
	_ = s.store.SetQueueStatus(ctx, row.ID, store.QueueImported, "")
	// Remove using the client id the item actually landed on (the queue row may
	// have been enqueued without an explicit client override).
	if item.DownloadClientID != "" && item.ID != "" {
		_ = s.queue.Remove(ctx, item.DownloadClientID, item.ID, false)
	}
	s.emit(ctx, ImportCompletedEvent{QueueID: row.ID, Status: store.QueueImported})
	s.emit(ctx, QueueUpdated{ID: row.ID})
	return nil
}

// matchItem finds the live download-client item for a queue row. It keys on the
// client item id (which must be non-empty) and only constrains the client id
// when the row recorded one — a row enqueued without an explicit client override
// (DownloadClientID == "") matches purely on the item id, since Grab routes by
// protocol/priority and the landing client id is only known from the live item.
func matchItem(items []provider.DownloadItem, row store.QueueItem) (provider.DownloadItem, bool) {
	if row.ClientItemID == "" {
		return provider.DownloadItem{}, false
	}
	for _, it := range items {
		if it.ID != row.ClientItemID {
			continue
		}
		if row.DownloadClientID != "" && it.DownloadClientID != row.DownloadClientID {
			continue
		}
		return it, true
	}
	return provider.DownloadItem{}, false
}

// allTargetsHaveFiles reports whether every targeted episode (or the movie) now
// has a media_files row.
func (s *Service) allTargetsHaveFiles(ctx context.Context, row store.QueueItem, kind provider.MediaKind) bool {
	if kind == provider.KindMovie {
		mf, _ := s.store.MediaFileForMovie(ctx, *row.MovieID)
		return mf != nil
	}
	for _, id := range row.EpisodeIDs {
		mf, _ := s.store.MediaFileForEpisode(ctx, id)
		if mf == nil {
			return false
		}
	}
	return true
}

// importTarget is the resolved destination for one video file.
type importTarget struct {
	episodeID *int64
	movieID   *int64
	dst       string
}

// importFile resolves the target for one video file and imports it, honoring
// upgrades. Returns (imported, error): imported is false when the file was a
// deliberate skip (no target match, or not an upgrade) that should not fail the
// whole row on its own.
func (s *Service) importFile(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, srcFile string) (bool, error) {
	parsed := parsing.Parse(filepath.Base(srcFile), kind)
	q := quality.Resolve(parsed)
	if q.ID == 0 {
		if d, ok := quality.DefinitionByID(row.QualityID); ok {
			q = d
		}
	}
	ext := filepath.Ext(srcFile)

	target, profile, mf, err := s.resolveTarget(ctx, row, kind, cfg, parsed, q, ext)
	if err != nil {
		return false, err
	}
	if target == nil {
		return false, nil // no matching library item for this file — skip
	}

	var existing *store.MediaFile
	if target.episodeID != nil {
		existing, _ = s.store.MediaFileForEpisode(ctx, *target.episodeID)
	} else {
		existing, _ = s.store.MediaFileForMovie(ctx, *target.movieID)
	}
	if existing != nil {
		if !quality.IsUpgrade(q.ID, existing.QualityID, profile) {
			qid := q.ID
			_ = s.store.AddHistory(ctx, store.HistoryEvent{
				EventType: "import_failed", MediaKind: row.MediaKind, SeriesID: row.SeriesID,
				MovieID: row.MovieID, EpisodeID: target.episodeID, SourceTitle: row.SourceTitle,
				QualityID: &qid, Message: "not an upgrade",
			})
			return false, nil
		}
	}

	if err := placeFile(srcFile, target.dst); err != nil {
		return false, err
	}
	root := s.mustRoot(ctx, row, kind)
	rel, err := filepath.Rel(root, target.dst)
	if err != nil {
		rel = target.dst
	}
	fi, _ := os.Stat(target.dst)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	mf.RelativePath = filepath.ToSlash(rel)
	mf.Size = size
	mf.QualityID = q.ID
	if _, err := s.store.UpsertMediaFile(ctx, mf); err != nil {
		return false, err
	}
	if existing != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(existing.RelativePath)))
	}
	evt := "imported"
	if existing != nil {
		evt = "upgraded"
	}
	qid := q.ID
	_ = s.store.AddHistory(ctx, store.HistoryEvent{
		EventType: evt, MediaKind: row.MediaKind, SeriesID: row.SeriesID, MovieID: row.MovieID,
		EpisodeID: target.episodeID, SourceTitle: row.SourceTitle, QualityID: &qid,
	})
	return true, nil
}

// resolveTarget builds the destination path + media_files template for one file.
func (s *Service) resolveTarget(ctx context.Context, row store.QueueItem, kind provider.MediaKind, cfg naming.Config, parsed parsing.ParsedRelease, q quality.QualityDefinition, ext string) (*importTarget, store.QualityProfile, store.MediaFile, error) {
	if kind == provider.KindMovie {
		m, err := s.store.GetMovie(ctx, *row.MovieID)
		if err != nil {
			return nil, store.QualityProfile{}, store.MediaFile{}, err
		}
		profile, _ := s.profileFor(ctx, kind, 0, *row.MovieID)
		root, err := s.rootPath(ctx, m.RootFolderID)
		if err != nil {
			return nil, store.QualityProfile{}, store.MediaFile{}, err
		}
		tok := naming.Tokens{MovieTitle: m.Title, Year: m.Year, Quality: q.Name, ReleaseGroup: parsed.ReleaseGroup}
		dst := filepath.Join(root, naming.Sanitize(naming.Render(cfg.MovieFolder, tok)), naming.Sanitize(naming.Render(cfg.MovieFile, tok))+ext)
		return &importTarget{movieID: row.MovieID, dst: dst}, profile, store.MediaFile{MediaKind: "movie", MovieID: row.MovieID}, nil
	}

	se, err := s.store.GetSeries(ctx, *row.SeriesID)
	if err != nil {
		return nil, store.QualityProfile{}, store.MediaFile{}, err
	}
	profile, _ := s.profileFor(ctx, kind, *row.SeriesID, 0)
	root, err := s.rootPath(ctx, se.RootFolderID)
	if err != nil {
		return nil, store.QualityProfile{}, store.MediaFile{}, err
	}
	ep := s.matchEpisode(ctx, row.EpisodeIDs, parsed)
	if ep == nil {
		return nil, profile, store.MediaFile{}, nil
	}
	tok := naming.Tokens{
		SeriesTitle: se.Title, EpisodeTitle: ep.Title, Quality: q.Name,
		ReleaseGroup: parsed.ReleaseGroup, Season: ep.SeasonNumber, Episode: ep.EpisodeNumber,
	}
	dst := filepath.Join(root,
		naming.Sanitize(naming.Render(cfg.SeriesFolder, tok)),
		naming.Sanitize(naming.Render(cfg.SeasonFolder, tok)),
		naming.Sanitize(naming.Render(cfg.EpisodeFile, tok))+ext)
	epID := ep.ID
	return &importTarget{episodeID: &epID, dst: dst}, profile, store.MediaFile{MediaKind: "tv", EpisodeID: &epID}, nil
}

// matchEpisode returns the recorded episode whose season+number matches the
// parse. For a single-file download with one recorded episode and no S/E in the
// parse, it returns that episode.
func (s *Service) matchEpisode(ctx context.Context, episodeIDs []int64, parsed parsing.ParsedRelease) *store.Episode {
	if len(episodeIDs) == 1 && len(parsed.Episodes) == 0 {
		ep, err := s.store.GetEpisode(ctx, episodeIDs[0])
		if err != nil {
			return nil
		}
		return ep
	}
	for _, id := range episodeIDs {
		ep, err := s.store.GetEpisode(ctx, id)
		if err != nil {
			continue
		}
		for _, n := range parsed.Episodes {
			if ep.SeasonNumber == parsed.Season && ep.EpisodeNumber == n {
				return ep
			}
		}
	}
	return nil
}

func (s *Service) mustRoot(ctx context.Context, row store.QueueItem, kind provider.MediaKind) string {
	if kind == provider.KindMovie {
		if m, err := s.store.GetMovie(ctx, *row.MovieID); err == nil {
			if p, err := s.rootPath(ctx, m.RootFolderID); err == nil {
				return p
			}
		}
		return ""
	}
	if se, err := s.store.GetSeries(ctx, *row.SeriesID); err == nil {
		if p, err := s.rootPath(ctx, se.RootFolderID); err == nil {
			return p
		}
	}
	return ""
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
