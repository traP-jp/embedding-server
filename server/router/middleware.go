package router

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"embedding-server/api/api"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v5"
	mid "github.com/labstack/echo/v5/middleware"
	echomiddleware "github.com/oapi-codegen/echo-v5-middleware"
)

type APIKeyAuthConfig struct {
	ExternalAPIKey string
	InternalAPIKey string
	Disabled       bool
}

// UseMiddleware は共通 HTTP ミドルウェア（CORS・リクエストログ・認証・OpenAPI 検証）を登録する。
func UseMiddleware(e *echo.Echo, auth APIKeyAuthConfig) error {
	e.Use(mid.CORSWithConfig(mid.CORSConfig{
		AllowOrigins: []string{
			"https://api-embeddings.mumumu6.net",
			"http://localhost:8081",
			"http://127.0.0.1:8081",
		},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))
	e.Use(mid.RequestLoggerWithConfig(mid.RequestLoggerConfig{
		LogLatency:    true,
		LogMethod:     true,
		LogURI:        true,
		LogStatus:     true,
		LogRemoteIP:   true,
		LogRequestID:  true,
		HandleError:   true,
		LogValuesFunc: requestLogValues,
	}))
	// OpenAPI バリデータがボディを読み込む前に、HTTP リクエスト全体のサイズを制限する。
	// 画像は1枚20 MiBを4枚までとし、multipartの付加情報を許容する。
	// JSONエンドポイントはそれより小さい上限にする。
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		jsonHandler := mid.BodyLimit(1 << 20)(next)
		imageHandler := mid.BodyLimit(81 << 20)(next)
		return func(c *echo.Context) error {
			switch c.Request().URL.Path {
			case "/v1/embeddings/images", "/v1/embeddings/multimodal":
				return imageHandler(c)
			}
			return jsonHandler(c)
		}
	})

	swagger, err := api.GetSpec()
	if err != nil {
		return fmt.Errorf("load openapi spec: %w", err)
	}
	e.Use(echomiddleware.OapiRequestValidatorWithOptions(swagger, &echomiddleware.Options{
		DoNotValidateServers: true,
		Options:              openapi3filter.Options{AuthenticationFunc: apiKeyAuthenticationFunc(auth)},
	}))
	return nil
}

func apiKeyAuthenticationFunc(cfg APIKeyAuthConfig) openapi3filter.AuthenticationFunc {
	externalExpected := []byte(cfg.ExternalAPIKey)
	internalExpected := []byte(cfg.InternalAPIKey)
	return func(ctx context.Context, input *openapi3filter.AuthenticationInput) error {
		if cfg.Disabled {
			return nil
		}

		var expected []byte
		switch input.SecuritySchemeName {
		case "ExternalBearerAuth":
			expected = externalExpected
		case "InternalBearerAuth":
			expected = internalExpected
		default:
			return fmt.Errorf("unsupported security scheme: %s", input.SecuritySchemeName)
		}

		got := requestAPIKey(input.RequestValidationInput.Request)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			if c := echomiddleware.GetEchoContext(ctx); c != nil {
				c.Response().Header().Set("WWW-Authenticate", "Bearer")
			}
			return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
		}
		return nil
	}
}

func requestAPIKey(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
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
