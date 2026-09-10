package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	AppEnv       string `envconfig:"APP_ENV" required:"true"`
	APIPort      string `envconfig:"API_PORT" required:"true"`
	AuthDisabled bool   `envconfig:"AUTH_DISABLED" default:"false"`
	// APIKey はクライアントからの公開 API 認証と webhook 送信に使う秘密。
	APIKey string `envconfig:"API_KEY"`
	// InternalAPIKey は worker の /internal 認証と Modal 起動に使う秘密。
	InternalAPIKey string   `envconfig:"INTERNAL_API_KEY"`
	Database       DBConfig `envconfig:"POSTGRES"`
	S3             S3Config `envconfig:"S3"`
	Modal          ModalConfig
}

type ModalConfig struct {
	// Enable が false のとき Modal 起動を行わない（URL があっても無効）。
	Enable bool `envconfig:"MODAL_ENABLE" default:"true"`
	// TriggerURL は Modal の run_batch HTTP endpoint（deploy 後に出る URL）。
	TriggerURL string `envconfig:"MODAL_TRIGGER_URL"`
	// BatchThreshold は Modal を起こす pending 画像ジョブ数の閾値。
	// 推論の EMBEDDING_BATCH_SIZE とは別（こちらは「何件溜まったら起動するか」）。
	// 起動後の Modal は text も claim する（画像が重いので起動し、起きている間に text も消化）。
	BatchThreshold int `envconfig:"MODAL_BATCH_THRESHOLD" default:"10"`
	// MinInterval は連続 trigger の最短間隔（二重起動防止）。
	MinInterval time.Duration `envconfig:"MODAL_MIN_INTERVAL" default:"30s"`
	// TriggerTimeout は run_batch への HTTP POST のタイムアウト。
	TriggerTimeout time.Duration `envconfig:"MODAL_TRIGGER_TIMEOUT" default:"15s"`
	// ReclaimTTL は processing のまま放置されたジョブを pending に戻すまでの時間。
	ReclaimTTL time.Duration `envconfig:"MODAL_RECLAIM_TTL" default:"30m"`
	// ReclaimEvery は stale reclaim を回す間隔。
	ReclaimEvery time.Duration `envconfig:"MODAL_RECLAIM_EVERY" default:"1m"`
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
	cfg.AppEnv = strings.TrimSpace(cfg.AppEnv)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.InternalAPIKey = strings.TrimSpace(cfg.InternalAPIKey)

	if cfg.AuthDisabled {
		if strings.EqualFold(cfg.AppEnv, "production") {
			return Config{}, fmt.Errorf("AUTH_DISABLED must not be true in production")
		}
		return cfg, nil
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("API_KEY must not be empty when authentication is enabled")
	}
	if cfg.InternalAPIKey == "" {
		return Config{}, fmt.Errorf("INTERNAL_API_KEY must not be empty when authentication is enabled")
	}
	if cfg.APIKey == cfg.InternalAPIKey {
		return Config{}, fmt.Errorf("API_KEY and INTERNAL_API_KEY must be different")
	}

	return cfg, nil
}
