package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"embedding-server/api/api"
	"embedding-server/api/config"
	"embedding-server/api/repository/gormrepo"
	"embedding-server/api/router"
	"embedding-server/api/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: cfg.AppEnv == "debug",
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))

	db, err := gormrepo.GetDBClient(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var background sync.WaitGroup
	defer func() {
		stop()
		background.Wait()
	}()

	jobFile, err := service.NewS3JobFileService(ctx, cfg.S3)
	if err != nil {
		return fmt.Errorf("initialize job image object storage: %w", err)
	}

	notifier := service.NewLocalJobNotifier()
	webhook := service.NewWebhookDispatcher()
	repo := gormrepo.GetRepository(db)
	modalTrigger := service.NewModalTrigger(service.ModalTriggerConfig{
		Enable:         cfg.Modal.Enable,
		URL:            cfg.Modal.TriggerURL,
		APIKey:         cfg.InternalAPIKey,
		BatchThreshold: cfg.Modal.BatchThreshold,
		MinInterval:    cfg.Modal.MinInterval,
		TriggerTimeout: cfg.Modal.TriggerTimeout,
		ReclaimTTL:     cfg.Modal.ReclaimTTL,
		ReclaimEvery:   cfg.Modal.ReclaimEvery,
	}, repo)
	embedding := service.NewEmbeddingService(repo, notifier, jobFile, webhook, modalTrigger)
	handlers := router.NewHandlers(repo, notifier, embedding, jobFile)
	strictHandlers := api.NewStrictHandler(handlers, nil)

	e := echo.New()
	if err := router.UseMiddleware(e, router.APIKeyAuthConfig{
		ExternalAPIKey: cfg.APIKey,
		InternalAPIKey: cfg.InternalAPIKey,
		Disabled:       cfg.AuthDisabled,
	}); err != nil {
		return fmt.Errorf("configure middleware: %w", err)
	}
	api.RegisterHandlers(e, strictHandlers)

	cleanup := service.NewCleanupService(repo, jobFile)
	
	background.Go(func() {
		cleanup.Run(ctx)
	})
	background.Go(func() {
		modalTrigger.RunReclaimLoop(ctx)
	})

	startConfig := echo.StartConfig{
		Address:         ":" + cfg.APIPort,
		GracefulTimeout: 10 * time.Second,
	}
	if err := startConfig.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}
