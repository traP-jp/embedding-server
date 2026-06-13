package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/labstack/echo/v5"
	mid "github.com/labstack/echo/v5/middleware"

	"embedding-server/api/api"
	"embedding-server/api/repository/gormrepo"
	"embedding-server/api/router"
	"embedding-server/api/service"
)

func main() {
	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: os.Getenv("APP_ENV") == "debug",
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))

	db, err := gormrepo.GetDBClient()
	if err != nil {
		slog.Error("failed to connect database", slog.Any("error", err))
		os.Exit(1)
	}

	jobDataDir := os.Getenv("JOB_DATA_DIR")
	if jobDataDir == "" {
		jobDataDir = "/data/jobs"
	}
	jobFile := service.NewJobFileService(jobDataDir)

	notifier := service.NewLocalJobNotifier()

	repo := gormrepo.GetRepository(db)
	embedding := service.NewEmbeddingService(repo, notifier, jobFile)
	handlers := router.NewHandlers(repo, notifier, embedding, jobFile)
	strictHandlers := api.NewStrictHandler(handlers, nil)

	ctx := context.Background()

	cleanup := service.NewCleanupService(jobFile.DataDir(), repo)
	go cleanup.Run(ctx)

	e := echo.New()

	e.Use(mid.RequestLoggerWithConfig(mid.RequestLoggerConfig{
		LogLatency:   true,
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogRemoteIP:  true,
		LogRequestID: true,
		HandleError:  true,
		LogValuesFunc: func(c *echo.Context, v mid.RequestLoggerValues) error {
			// ワーカーのポーリングはジョブがない場合に204を返すため、正常系のログ出力を抑制する。
			if v.Error == nil && v.Method == http.MethodPost && v.URI == "/internal/worker/jobs/claim" && v.Status == http.StatusNoContent {
				return nil
			}

			attrs := []any{
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.String("latency", v.Latency.String()),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),
			}
			if v.Error == nil {
				slog.Info("request", attrs...)
				return nil
			}

			attrs = append(attrs, slog.Any("error", v.Error))
			slog.Error("request", attrs...)
			return nil
		},
	}))
	api.RegisterHandlers(e, strictHandlers)

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("listening", slog.String("port", port))
	if err := e.Start(":" + port); err != nil {
		slog.Error("server error", slog.Any("error", err))
		os.Exit(1)
	}
}
