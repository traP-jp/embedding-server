package main

import (
	"fmt"
	"log"
	"os"

	"github.com/labstack/echo/v4"
	mid "github.com/labstack/echo/v4/middleware"

	"embedding-server/api/api"
	"embedding-server/api/repository/gormrepo"
	"embedding-server/api/router"
	"embedding-server/api/service"
)

func main() {
	db, err := gormrepo.GetDBClient()
	if err != nil {
		log.Fatal(err)
	}
	notifier := service.NewLocalJobNotifier()

	repo := gormrepo.GetRepository(db)
	embedding := service.NewEmbeddingService(repo, notifier)
	handlers := router.GetHandlers(repo, notifier, embedding)
	strictHandlers := api.NewStrictHandler(handlers, nil)

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
			message := fmt.Sprintf("%s %s status=%d latency=%s", v.Method, v.URI, v.Status, v.Latency)
			if v.Error == nil {
				c.Logger().Info(message)
				return nil
			}

			c.Logger().Errorf("%s err=%v", message, v.Error)
			return v.Error
		},
	}))
	api.RegisterHandlers(e, strictHandlers)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on %s", port)
	e.Logger.Fatal(e.Start(":" + port))
}
