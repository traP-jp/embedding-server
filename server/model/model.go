package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"embedding-server/api/repository"
)

// EmbeddingJob は埋め込みジョブ行（GORM AutoMigrate で作成）。
type EmbeddingJob struct {
	ID        uuid.UUID                     `gorm:"type:uuid;primaryKey"`
	Payload   datatypes.JSON                `gorm:"type:jsonb;not null"`
	Result    datatypes.JSON                `gorm:"type:jsonb"`
	Status    repository.EmbeddingJobStatus `gorm:"not null;default:pending;index"`
	CreatedAt time.Time                     `gorm:"not null;autoCreateTime"`
	StartedAt *time.Time
}

func (j *EmbeddingJob) BeforeCreate(_ *gorm.DB) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	return nil
}

// EmbeddingCache は embedding_caches テーブル（LRU で削除する内部キャッシュ）。
type EmbeddingCache struct {
	Key            string         `gorm:"primaryKey"`
	Value          datatypes.JSON `gorm:"type:jsonb;not null"`
	LastAccessedAt time.Time      `gorm:"not null;autoCreateTime;default:CURRENT_TIMESTAMP;index"`
}

// migrate で自動的にテーブル作成されるモデルのリスト。
func Models() []any {
	return []any{
		&EmbeddingJob{},
		&EmbeddingCache{},
	}
}
