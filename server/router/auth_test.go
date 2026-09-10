package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v5"
	echomiddleware "github.com/oapi-codegen/echo-v5-middleware"
)

func TestAPIKeyAuthenticationFunc(t *testing.T) {
	authenticate := apiKeyAuthenticationFunc(APIKeyAuthConfig{
		ExternalAPIKey: "external-secret",
		InternalAPIKey: "internal-secret",
	})

	for name, tt := range map[string]struct {
		scheme string
		key    string
		wantOK bool
	}{
		"external key":                     {scheme: "ExternalBearerAuth", key: "external-secret", wantOK: true},
		"internal key":                     {scheme: "InternalBearerAuth", key: "internal-secret", wantOK: true},
		"external key rejected internally": {scheme: "InternalBearerAuth", key: "external-secret"},
		"internal key rejected externally": {scheme: "ExternalBearerAuth", key: "internal-secret"},
		"missing key":                      {scheme: "ExternalBearerAuth"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			if tt.key != "" {
				req.Header.Set("Authorization", "Bearer "+tt.key)
			}
			input := &openapi3filter.AuthenticationInput{
				SecuritySchemeName:     tt.scheme,
				RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req},
			}
			err := authenticate(context.Background(), input)
			if (err == nil) != tt.wantOK {
				t.Fatalf("error=%v, wantOK=%v", err, tt.wantOK)
			}
		})
	}
}

func TestAPIKeyAuthenticationFuncUnauthorizedResponse(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/test", nil), httptest.NewRecorder())
	input := &openapi3filter.AuthenticationInput{
		SecuritySchemeName:     "ExternalBearerAuth",
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: c.Request()},
	}

	err := apiKeyAuthenticationFunc(APIKeyAuthConfig{ExternalAPIKey: "secret"})(
		context.WithValue(context.Background(), echomiddleware.EchoContextKey, c), input,
	)
	if err == nil || echoErrCode(err) != http.StatusUnauthorized {
		t.Fatalf("error=%v, want unauthorized", err)
	}
	if got := c.Response().Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate=%q", got)
	}
}

func TestAPIKeyAuthenticationFuncDisabled(t *testing.T) {
	input := &openapi3filter.AuthenticationInput{
		SecuritySchemeName: "InternalBearerAuth",
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: httptest.NewRequest(http.MethodPost, "/test", nil),
		},
	}
	if err := apiKeyAuthenticationFunc(APIKeyAuthConfig{Disabled: true})(context.Background(), input); err != nil {
		t.Fatalf("error=%v", err)
	}
}

func echoErrCode(err error) int {
	if httpErr, ok := err.(*echo.HTTPError); ok {
		return httpErr.Code
	}
	return 0
}
