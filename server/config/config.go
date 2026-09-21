package config

import (
	"fmt"
	"strings"
	"time"

	env "github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv       string `env:"APP_ENV,required,notEmpty"`
	APIPort      string `env:"API_PORT,required,notEmpty"`
	AuthDisabled bool   `env:"AUTH_DISABLED" envDefault:"false"`
	// APIKey はクライアントからの公開 API 認証に使う秘密。
	APIKey string `env:"API_KEY"`
	// InternalAPIKey は worker の /internal 認証と Modal 起動に使う秘密。
	InternalAPIKey string   `env:"INTERNAL_API_KEY"`
	Database       DBConfig `envPrefix:"POSTGRES_"`
	S3             S3Config `envPrefix:"S3_"`
	Modal          ModalConfig
}

type ModalConfig struct {
	// Enable が false のとき Modal 起動を行わない（URL があっても無効）。
	Enable bool `env:"MODAL_ENABLE" envDefault:"true"`
	// TriggerURL は Modal の run_batch HTTP endpoint（deploy 後に出る URL）。
	TriggerURL string `env:"MODAL_TRIGGER_URL"`
	// BatchThreshold は Modal を起こす pending 画像ジョブ数の閾値。
	// 推論の EMBEDDING_BATCH_SIZE とは別（こちらは「何件溜まったら起動するか」）。
	// 起動後の Modal は text も claim する（画像が重いので起動し、起きている間に text も消化）。
	BatchThreshold int `env:"MODAL_BATCH_THRESHOLD" envDefault:"10"`
	// MinInterval は連続 trigger の最短間隔（二重起動防止）。
	MinInterval time.Duration `env:"MODAL_MIN_INTERVAL" envDefault:"30s"`
	// TriggerTimeout は run_batch への HTTP POST のタイムアウト。
	TriggerTimeout time.Duration `env:"MODAL_TRIGGER_TIMEOUT" envDefault:"15s"`
	// ReclaimTTL は processing のまま放置されたジョブを pending に戻すまでの時間。
	ReclaimTTL time.Duration `env:"MODAL_RECLAIM_TTL" envDefault:"30m"`
	// ReclaimEvery は stale reclaim を回す間隔。
	ReclaimEvery time.Duration `env:"MODAL_RECLAIM_EVERY" envDefault:"1m"`
}

type DBConfig struct {
	Host     string `env:"HOST,required,notEmpty"`
	Port     string `env:"PORT,required,notEmpty"`
	User     string `env:"USER,required,notEmpty"`
	Password string `env:"PASSWORD,required,notEmpty"`
	DBName   string `env:"DB,required,notEmpty"`
	SSLMode  string `env:"SSLMODE,required,notEmpty"`
}

type S3Config struct {
	Endpoint        string `env:"ENDPOINT_URL,required,notEmpty"`
	Bucket          string `env:"BUCKET,required,notEmpty"`
	Region          string `env:"REGION,required,notEmpty"`
	AccessKeyID     string `env:"ACCESS_KEY_ID,required,notEmpty"`
	SecretAccessKey string `env:"SECRET_ACCESS_KEY,required,notEmpty"`
	Prefix          string `env:"PREFIX,required,notEmpty"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
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
