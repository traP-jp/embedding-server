package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	mid "github.com/labstack/echo/v4/middleware"

	"embedding-server/api/api"
	"embedding-server/api/repository/gormrepo"
	"embedding-server/api/router"
	"embedding-server/api/service"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

func main() {
	db, err := gormrepo.GetDBClient()
	if err != nil {
		logger.Error("failed to connect database", slog.Any("error", err))
		os.Exit(1)
	}
	notifier := service.NewLocalJobNotifier()

	repo := gormrepo.GetRepository(db)
	embedding := service.NewEmbeddingService(repo, notifier)
	handlers := router.GetHandlers(repo, notifier, embedding)
	strictHandlers := api.NewStrictHandler(handlers, nil)

	ctx := context.Background()

	cleanup := service.NewCleanupService("/data/jobs", repo)
	go cleanup.Run(ctx)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Debug = os.Getenv("DEBUG") == "true"

	e.Use(mid.RequestLoggerWithConfig(mid.RequestLoggerConfig{
		LogLatency:   true,
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogError:     true,
		LogRemoteIP:  true,
		LogRequestID: true,
		HandleError:  true,
		LogValuesFunc: func(c echo.Context, v mid.RequestLoggerValues) error {
			attrs := []any{
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),
			}
			if v.Error == nil {
				logger.Info("request", attrs...)
				return nil
			}

			attrs = append(attrs, slog.Any("error", v.Error))
			logger.Error("request", attrs...)
			return v.Error
		},
	}))
	api.RegisterHandlers(e, strictHandlers)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("listening", slog.String("port", port))
	e.Logger.Fatal(e.Start(":" + port))
}
