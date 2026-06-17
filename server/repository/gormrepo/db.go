package gormrepo

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"embedding-server/api/config"
	"embedding-server/api/model"
)

func GetDBClient(cfg config.DBConfig) (*gorm.DB, error) {
	dsn := postgresDSN(cfg)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      true,
				Colorful:                  false,
			},
		),
	})
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

func postgresDSN(cfg config.DBConfig) string {
	connURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   cfg.DBName,
	}
	q := connURL.Query()
	q.Set("sslmode", cfg.SSLMode)
	connURL.RawQuery = q.Encode()
	return connURL.String()
}
