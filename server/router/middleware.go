package router

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"embedding-server/api/api"

	"github.com/labstack/echo/v5"
	mid "github.com/labstack/echo/v5/middleware"
	echomiddleware "github.com/oapi-codegen/echo-v5-middleware"
)

// UseMiddleware は共通 HTTP ミドルウェア（リクエストログ・API キー認証・OpenAPI 検証）を登録する。
func UseMiddleware(e *echo.Echo, apiKey string) error {
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

	e.Use(apiKeyAuth(apiKey))

	swagger, err := api.GetSpec()
	if err != nil {
		return fmt.Errorf("load openapi spec: %w", err)
	}
	e.Use(echomiddleware.OapiRequestValidatorWithOptions(swagger, &echomiddleware.Options{
		DoNotValidateServers: true,
	}))
	return nil
}

func apiKeyAuth(apiKey string) echo.MiddlewareFunc {
	expected := []byte(apiKey)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			got := requestAPIKey(c.Request())
			if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
			}
			return next(c)
		}
	}
}

func requestAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
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
