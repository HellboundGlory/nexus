package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/hellboundg/nexus/internal/core/api"
	"github.com/hellboundg/nexus/internal/core/auth"
	"github.com/hellboundg/nexus/internal/core/command"
	"github.com/hellboundg/nexus/internal/core/config"
	"github.com/hellboundg/nexus/internal/core/database"
	"github.com/hellboundg/nexus/internal/core/events"
	"github.com/hellboundg/nexus/internal/core/logging"
	"github.com/hellboundg/nexus/internal/core/scheduler"
	"github.com/hellboundg/nexus/internal/core/store"
	"github.com/hellboundg/nexus/internal/core/version"
	"github.com/hellboundg/nexus/internal/indexer"
	"github.com/hellboundg/nexus/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
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
	idxAPI := indexer.NewAPI(st, idxSvc, nil)

	sch := scheduler.New(mgr)
	sch.Every(15*time.Minute, func() command.Command {
		return indexer.NewHealthCheck(st, bus, nil)
	})
	sch.Start()

	authSvc := auth.NewService(st, cfg.APIKey)
	router := api.NewRouter(api.Deps{
		Auth: authSvc, Store: st, Version: version.Version(), Bus: bus,
		WSForward: []string{"indexer.status"},
	}, web.Handler(), idxAPI.Mount)

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
