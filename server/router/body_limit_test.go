package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestBodyLimitRunsBeforeOpenAPIValidation(t *testing.T) {
	for _, path := range []string{"/v1/embeddings/text", "/v1/embeddings/images", "/v1/embeddings/multimodal"} {
		t.Run(path, func(t *testing.T) {
			e := echo.New()
			if err := UseMiddleware(e, APIKeyAuthConfig{Disabled: true}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("invalid"))
			req.ContentLength = 82 << 20
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBodyLimitHandlesUnknownContentLength(t *testing.T) {
	e := echo.New()
	if err := UseMiddleware(e, APIKeyAuthConfig{Disabled: true}); err != nil {
		t.Fatal(err)
	}
	e.POST("/v1/embeddings/text", func(c *echo.Context) error {
		t.Fatal("oversized body reached handler")
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings/text", strings.NewReader(`{"text":"`+strings.Repeat("a", 2<<20)+`"}`))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	// OpenAPI may wrap the read error as 400; it must never reach the handler.
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
