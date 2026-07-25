package router

import (
	"fmt"

	"embedding-server/api/api"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/oapi-codegen/echo-v5-middleware"
)

// UseOpenAPIRequestValidator は OpenAPI 仕様に基づくリクエスト検証ミドルウェアを登録する。
func UseOpenAPIRequestValidator(e *echo.Echo) error {
	swagger, err := api.GetSpec()
	if err != nil {
		return fmt.Errorf("load openapi spec: %w", err)
	}

	e.Use(echomiddleware.OapiRequestValidatorWithOptions(swagger, &echomiddleware.Options{
		DoNotValidateServers: true,
	}))
	return nil
}
