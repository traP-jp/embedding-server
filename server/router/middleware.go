package router

import (
	"fmt"
	"log/slog"
	"net/http"

	"embedding-server/api/api"

	"github.com/labstack/echo/v5"
	mid "github.com/labstack/echo/v5/middleware"
	echomiddleware "github.com/oapi-codegen/echo-v5-middleware"
)

// UseMiddleware は共通 HTTP ミドルウェア（リクエストログ・OpenAPI 検証）を登録する。
func UseMiddleware(e *echo.Echo) error {
	e.Use(mid.RequestLoggerWithConfig(mid.RequestLoggerConfig{
		LogLatency:   true,
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogRemoteIP:  true,
		LogRequestID: true,
		HandleError:  true,
		LogValuesFunc: requestLogValues,
	}))

	swagger, err := api.GetSpec()
	if err != nil {
		return fmt.Errorf("load openapi spec: %w", err)
	}
	e.Use(echomiddleware.OapiRequestValidatorWithOptions(swagger, &echomiddleware.Options{
		DoNotValidateServers: true,
	}))
	return nil
}

func requestLogValues(_ *echo.Context, v mid.RequestLoggerValues) error {
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
}
