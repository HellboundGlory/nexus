package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/hellboundg/nexus/internal/automation"
	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/config"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/logging"
	"github.com/hellboundg/nexus/internal/core/provider"
	"github.com/hellboundg/nexus/internal/core/scheduler"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/core/version"
	"github.com/hellboundg/nexus/internal/downloadclient"
	"github.com/hellboundg/nexus/internal/importing"
	"github.com/hellboundg/nexus/internal/indexer"
	"github.com/hellboundg/nexus/internal/media"
	"github.com/hellboundg/nexus/internal/quality"
	"github.com/hellboundg/nexus/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// healthcheck performs a one-shot GET of the local /health endpoint and returns
// a process exit code (0 healthy, 1 unhealthy). It exists so the container image
// can define a HEALTHCHECK without a shell or curl (the distroless base has
// neither). It honours NEXUS_PORT so it matches the running server's listener.
func healthcheck() int {
	port := 9494
	if v := os.Getenv("NEXUS_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "healthcheck: invalid NEXUS_PORT %q: %v\n", v, err)
			return 1
		}
		port = p
	}
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}

	logFile := &lumberjack.Logger{
		Filename: filepath.Join(cfg.DataDir, "nexus.log"), MaxSize: 10, MaxBackups: 3, MaxAge: 28,
	}
	defer logFile.Close()
	logWriter := io.MultiWriter(os.Stdout, logFile)
	log := logging.New(cfg.LogLevel, logWriter)
	slog.SetDefault(log)

	db, err := database.Open(filepath.Join(cfg.DataDir, "nexus.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		return err
	}

	st := store.New(db)
	if err := ensureAdmin(ctx, st, log); err != nil {
		return err
	}

	bus := events.New().WithLogger(log)
	mgr := command.NewManager(st, bus, 4).WithLogger(log)
	mgr.Start()

	idxSvc := indexer.NewService(st)
	if err := idxSvc.Reload(ctx); err != nil {
		return err
	}
	idxAPI := indexer.NewAPI(st, idxSvc, &http.Client{Timeout: 30 * time.Second})

	dlSvc := downloadclient.NewService(st)
	if err := dlSvc.Reload(ctx); err != nil {
		return err
	}
	dlAPI := downloadclient.NewAPI(st, dlSvc)
	dlMonitor := downloadclient.NewMonitor(dlSvc, bus)

	tmdb := media.NewTMDBProvider(cfg.TMDBAPIKey)
	mediaSvc := media.NewService(st, tmdb).WithBus(bus)
	mediaAPI := media.NewAPI(st, mediaSvc)
	mediaRefresh := media.NewRefresh(mediaSvc)

	qualitySvc := quality.NewService(st)
	qualityAPI := quality.NewAPI(qualitySvc)

	importSvc := importing.NewService(st, dlSvc, dlQueueAdapter{svc: dlSvc}, bus)
	importAPI := importing.NewAPI(importSvc)
	importCmd := importing.NewImportCommand(importSvc)

	autoSvc := automation.NewService(st, autoSearchAdapter{svc: idxSvc}, importSvc, bus)
	importSvc.SetResearcher(autoSvc)
	autoAPI := automation.NewAPI(autoSvc, mgr)
	autoCfg, err := autoSvc.Config(ctx)
	if err != nil {
		return err
	}

	sch := scheduler.New(mgr)
	sch.Every(15*time.Minute, func() command.Command {
		return indexer.NewHealthCheck(st, bus, &http.Client{Timeout: 30 * time.Second})
	})
	sch.Every(30*time.Second, func() command.Command { return dlMonitor })
	sch.Every(12*time.Hour, func() command.Command { return mediaRefresh })
	// 5s so a failed download is blocklisted and its replacement grabbed
	// promptly. Guarded because the Manager has no dedupe and runs several
	// workers: a run that outlasts the interval would otherwise overlap itself
	// and could double-blocklist a failure and double-grab its replacement.
	importTick := command.Single(importCmd)
	sch.Every(5*time.Second, func() command.Command { return importTick })
	sch.Every(time.Duration(autoCfg.MissingSearchIntervalHours)*time.Hour, func() command.Command {
		return automation.NewMissingSearchCommand(autoSvc)
	})
	if autoCfg.RSSSyncEnabled {
		sch.Every(time.Duration(autoCfg.RSSSyncIntervalMinutes)*time.Minute, func() command.Command {
			return automation.NewRSSSyncCommand(autoSvc)
		})
	}
	if autoCfg.UpgradeSearchEnabled {
		sch.Every(time.Duration(autoCfg.UpgradeSearchIntervalHours)*time.Hour, func() command.Command {
			return automation.NewUpgradeSearchCommand(autoSvc)
		})
	}
	sch.Start()

	authSvc := auth.NewService(st, cfg.APIKey)
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status", "download.status", "media.series.updated", "media.movie.updated", "import.completed", "queue.updated", "automation.search.completed", "automation.rss.completed", "automation.upgrade.completed", "download.failed"},
	}, web.Handler(), idxAPI.Mount, dlAPI.Mount, mediaAPI.Mount, qualityAPI.Mount, importAPI.Mount, autoAPI.Mount)

	srv := &http.Server{Addr: cfg.Addr(), Handler: router}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		sch.Stop()
		mgr.Stop()
	}()

	log.Info("nexus starting", "addr", cfg.Addr(), "version", version.Version())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func ensureAdmin(ctx context.Context, st *store.Store, log *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	username := os.Getenv("NEXUS_ADMIN_USER")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("NEXUS_ADMIN_PASSWORD")
	generated := false
	if password == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		password = hex.EncodeToString(b)
		generated = true
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, username, hash); err != nil {
		return err
	}
	if generated {
		log.Warn("created initial admin user", "username", username, "password", password)
	} else {
		log.Info("created initial admin user", "username", username)
	}
	return nil
}

// dlQueueAdapter exposes downloadclient.Service.Queue()'s items (dropping the
// aggregate error wrapper) so importing's QueueReader interface is satisfied
// without importing the downloadclient package into internal/importing.
type dlQueueAdapter struct{ svc *downloadclient.Service }

func (a dlQueueAdapter) Queue(ctx context.Context) []provider.DownloadItem {
	return a.svc.Queue(ctx).Items
}

func (a dlQueueAdapter) Remove(ctx context.Context, clientID, itemID string, deleteData bool) error {
	return a.svc.Remove(ctx, clientID, itemID, deleteData)
}

// autoSearchAdapter flattens indexer.Service.Search's SearchResult into the
// ([]provider.Release, error) shape automation.Searcher expects, without
// importing the indexer package into internal/automation. Per-indexer errors are
// surfaced as a non-fatal aggregate error; the releases that succeeded are still
// returned.
type autoSearchAdapter struct{ svc *indexer.Service }

func (a autoSearchAdapter) Search(ctx context.Context, q provider.Query) ([]provider.Release, error) {
	res := a.svc.Search(ctx, q)
	if len(res.IndexerErrors) > 0 {
		return res.Releases, fmt.Errorf("automation: %d indexer error(s)", len(res.IndexerErrors))
	}
	return res.Releases, nil
}
