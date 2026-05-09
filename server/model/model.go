package model

import (
	"time"

	"gorm.io/datatypes"
)

// EmbeddingJob は埋め込みジョブ行（GORM AutoMigrate で作成）。
type EmbeddingJob struct {
	ID          int64          `gorm:"primaryKey"`
	Payload     datatypes.JSON `gorm:"not null"`
	Result      datatypes.JSON
	Status      string    `gorm:"not null;default:pending"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime"`
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// EmbeddingCache は embedding_cache テーブル（キー・JSON・任意の有効期限）。
type EmbeddingCache struct {
	Key       string         `gorm:"primaryKey"`
	Value     datatypes.JSON `gorm:"not null"`
	ExpiresAt *time.Time     `gorm:"column:expires_at"`
}

func (EmbeddingCache) TableName() string {
	return "embedding_cache"
}
