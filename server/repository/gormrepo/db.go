package gormrepo

import (
	"fmt"
	"os"
	"path/filepath"

	sqliteDriver "github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"embedding-server/api/model"
)

func GetDBClient() (*gorm.DB, string, error) {
	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath == "" {
		sqlitePath = "data/embedding.db"
	}
	sqlitePath, err := filepath.Abs(sqlitePath)
	if err != nil {
		return nil, "", err
	}

	sqliteDSN := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)",
		filepath.ToSlash(sqlitePath),
	)
	db, err := gorm.Open(sqliteDriver.Open(sqliteDSN), &gorm.Config{})
	if err != nil {
		return nil, "", fmt.Errorf("gorm open: %w", err)
	}

	if err := db.AutoMigrate(&model.EmbeddingJob{}, &model.EmbeddingCache{}); err != nil {
		return nil, "", fmt.Errorf("auto migrate: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("sql db: %w", err)
	}
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)

	return db, sqlitePath, nil
}
