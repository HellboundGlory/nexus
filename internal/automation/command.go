package automation

import (
	"context"
	"fmt"

	"github.com/hellboundg/nexus/internal/core/command"
)

type searchCommand struct {
	name string
	run  func(ctx context.Context) (int, error)
}

func (c *searchCommand) Name() string { return c.name }

func (c *searchCommand) Run(ctx context.Context, r command.Reporter) error {
	r.Progress(0, "searching")
	n, err := c.run(ctx)
	if err != nil {
		return err
	}
	r.Progress(100, fmt.Sprintf("%d grabbed", n))
	return nil
}

func NewSearchMovieCommand(svc *Service, movieID int64) command.Command {
	return &searchCommand{name: "SearchMovie", run: func(ctx context.Context) (int, error) {
		return svc.SearchMovie(ctx, movieID)
	}}
}

func NewSearchSeriesCommand(svc *Service, seriesID int64) command.Command {
	return &searchCommand{name: "SearchSeries", run: func(ctx context.Context) (int, error) {
		return svc.SearchSeries(ctx, seriesID)
	}}
}

func NewSearchSeasonCommand(svc *Service, seriesID int64, seasonNumber int) command.Command {
	return &searchCommand{name: "SearchSeason", run: func(ctx context.Context) (int, error) {
		return svc.SearchSeason(ctx, seriesID, seasonNumber)
	}}
}

func NewSearchEpisodeCommand(svc *Service, episodeID int64) command.Command {
	return &searchCommand{name: "SearchEpisode", run: func(ctx context.Context) (int, error) {
		return svc.SearchEpisode(ctx, episodeID)
	}}
}

// NewMissingSearchCommand is the scheduled sweep over monitored-missing items.
func NewMissingSearchCommand(svc *Service) command.Command {
	return &searchCommand{name: "MissingSearch", run: func(ctx context.Context) (int, error) {
		cfg, err := svc.Config(ctx)
		if err != nil {
			return 0, err
		}
		return svc.MissingSweep(ctx, cfg.MissingSearchBatchSize)
	}}
}

// NewRSSSyncCommand is the scheduled RSS poll over all enabled indexers.
func NewRSSSyncCommand(svc *Service) command.Command {
	return &searchCommand{name: "RSSSync", run: func(ctx context.Context) (int, error) {
		res, err := svc.RSSSync(ctx)
		return res.Grabbed, err
	}}
}

// NewUpgradeSearchCommand is the scheduled upgrade / cutoff-unmet sweep over
// monitored items whose existing file ranks below the profile cutoff.
func NewUpgradeSearchCommand(svc *Service) command.Command {
	return &searchCommand{name: "UpgradeSearch", run: func(ctx context.Context) (int, error) {
		cfg, err := svc.Config(ctx)
		if err != nil {
			return 0, err
		}
		return svc.UpgradeSweep(ctx, cfg.UpgradeSearchBatchSize)
	}}
}
