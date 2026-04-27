package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	sqliteDriver "github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"embedding-server/api/config"
	"embedding-server/api/model"
	"embedding-server/api/repo"
	"embedding-server/api/router"
	"embedding-server/api/server"
)

func main() {
	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + strings.TrimPrefix(p, ":")
	}

	sqlitePath, err := config.SQLitePathFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err != nil {
		log.Fatalf("mkdir db dir: %v", err)
	}

	defaultQueue := os.Getenv("PYTHON_WORKER_QUEUE")
	if defaultQueue == "" {
		defaultQueue = "embedding-jobs"
	}

	dsn := config.SQLiteGormDSN(sqlitePath)
	db, err := gorm.Open(sqliteDriver.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("gorm open: %v", err)
	}
	if err := db.AutoMigrate(&model.EmbeddingJob{}, &model.EmbeddingCache{}); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(0)

	rp := repo.New(db)
	rt := router.NewRouter(sqlDB, rp, defaultQueue)
	e := server.NewEcho(rt)

	log.Printf("listening on %s (sqlite=%s)", addr, sqlitePath)
	e.Logger.Fatal(e.Start(addr))
}
