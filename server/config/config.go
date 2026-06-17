package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	AppEnv   string   `envconfig:"APP_ENV" required:"true"`
	APIPort  string   `envconfig:"API_PORT" required:"true"`
	Database DBConfig `envconfig:"POSTGRES"`
	S3       S3Config `envconfig:"S3"`
}

type DBConfig struct {
	Host     string `envconfig:"HOST" required:"true"`
	Port     string `envconfig:"PORT" required:"true"`
	User     string `envconfig:"USER" required:"true"`
	Password string `envconfig:"PASSWORD" required:"true"`
	DBName   string `envconfig:"DB" required:"true"`
	SSLMode  string `envconfig:"SSLMODE" required:"true"`
}

type S3Config struct {
	Endpoint        string `envconfig:"ENDPOINT_URL" required:"true"`
	Bucket          string `envconfig:"BUCKET" required:"true"`
	Region          string `envconfig:"REGION" required:"true"`
	AccessKeyID     string `envconfig:"ACCESS_KEY_ID" required:"true"`
	SecretAccessKey string `envconfig:"SECRET_ACCESS_KEY" required:"true"`
	Prefix          string `envconfig:"PREFIX" required:"true"`
}

func Load() (Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return Config{}, fmt.Errorf("load config from environment: %w", err)
	}

	return cfg, nil
}
