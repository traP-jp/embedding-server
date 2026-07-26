package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/labstack/echo/v5"

	"embedding-server/api/api"
	"embedding-server/api/config"
	"embedding-server/api/repository/gormrepo"
	"embedding-server/api/router"
	"embedding-server/api/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: cfg.AppEnv == "debug",
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))

	db, err := gormrepo.GetDBClient(cfg.Database)
	if err != nil {
		slog.Error("failed to connect database", slog.Any("error", err))
		os.Exit(1)
	}

	jobFile, err := service.NewS3JobFileService(context.Background(), cfg.S3)
	if err != nil {
		slog.Error("failed to initialize job image object storage", slog.Any("error", err))
		os.Exit(1)
	}

	notifier := service.NewLocalJobNotifier()
	webhook := service.NewWebhookDispatcher()
	repo := gormrepo.GetRepository(db)
	modalTrigger := service.NewModalTrigger(service.ModalTriggerConfig{
		Enable:         cfg.Modal.Enable,
		URL:            cfg.Modal.TriggerURL,
		Token:          cfg.Modal.TriggerToken,
		BatchThreshold: cfg.Modal.BatchThreshold,
		MinInterval:    cfg.Modal.MinInterval,
		TriggerTimeout: cfg.Modal.TriggerTimeout,
		ReclaimTTL:     cfg.Modal.ReclaimTTL,
		ReclaimEvery:   cfg.Modal.ReclaimEvery,
	}, repo)
	embedding := service.NewEmbeddingService(repo, notifier, jobFile, webhook, modalTrigger)
	handlers := router.NewHandlers(repo, notifier, embedding, jobFile)
	strictHandlers := api.NewStrictHandler(handlers, nil)

	ctx := context.Background()

	cleanup := service.NewCleanupService(repo, jobFile)
	go cleanup.Run(ctx)
	go modalTrigger.RunReclaimLoop(ctx)

	e := echo.New()
	if err := router.UseMiddleware(e); err != nil {
		slog.Error("failed to configure middleware", slog.Any("error", err))
		os.Exit(1)
	}
	api.RegisterHandlers(e, strictHandlers)

	slog.Info("listening", slog.String("port", cfg.APIPort))
	if err := e.Start(":" + cfg.APIPort); err != nil {
		slog.Error("server error", slog.Any("error", err))
		os.Exit(1)
	}
}
