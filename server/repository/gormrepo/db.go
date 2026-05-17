package gormrepo

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"embedding-server/api/model"
)

func GetDBClient() (*gorm.DB, error) {
	host := getenv("POSTGRES_HOST", "postgres")
	port := getenv("POSTGRES_PORT", "5432")
	user := getenv("POSTGRES_USER", "postgres")
	password := getenv("POSTGRES_PASSWORD", "password")
	dbname := getenv("POSTGRES_DB", "embedding")
	sslmode := getenv("POSTGRES_SSLMODE", "disable")
	dsn := postgresDSN(user, password, host, port, dbname, sslmode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}

	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	// DB接続の確認
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := db.AutoMigrate(model.Models()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	return db, nil
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func postgresDSN(user, password, host, port, dbname, sslmode string) string {
	connURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, port),
		Path:   dbname,
	}
	q := connURL.Query()
	q.Set("sslmode", sslmode)
	connURL.RawQuery = q.Encode()
	return connURL.String()
}
