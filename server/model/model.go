package model

import (
	"time"

	"gorm.io/datatypes"
)

// EmbeddingJob は埋め込みジョブ行（GORM AutoMigrate で作成）。
type EmbeddingJob struct {
	ID          int64          `gorm:"primaryKey"`
	Payload     datatypes.JSON `gorm:"type:jsonb;not null"`
	Result      datatypes.JSON `gorm:"type:jsonb"`
	Status      string         `gorm:"not null;default:pending"`
	CreatedAt   time.Time      `gorm:"not null;autoCreateTime"`
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// EmbeddingCache は embedding_caches テーブル（キー・JSON・任意の有効期限）。
type EmbeddingCache struct {
	Key       string         `gorm:"primaryKey"`
	Value     datatypes.JSON `gorm:"type:jsonb;not null"`
	ExpiresAt *time.Time     `gorm:"column:expires_at"`
}

// migrate で自動的にテーブル作成されるモデルのリスト。
func Models() []any {
	return []any{
		&EmbeddingJob{},
		&EmbeddingCache{},
	}
}
