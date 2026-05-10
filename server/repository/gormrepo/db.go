package gormrepo

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"embedding-server/api/model"
)

func GetDBClient() (*gorm.DB, string, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		host := getenv("POSTGRES_HOST", "postgres")
		port := getenv("POSTGRES_PORT", "5432")
		user := getenv("POSTGRES_USER", "postgres")
		password := getenv("POSTGRES_PASSWORD", "password")
		dbname := getenv("POSTGRES_DB", "embedding")
		sslmode := getenv("POSTGRES_SSLMODE", "disable")
		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			host,
			port,
			user,
			password,
			dbname,
			sslmode,
		)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, "", fmt.Errorf("gorm open: %w", err)
	}

	if err := db.AutoMigrate(model.Models()...); err != nil {
		return nil, "", fmt.Errorf("auto migrate: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("sql db: %w", err)
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)

	return db, dsn, nil
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
