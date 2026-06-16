package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// EmbeddingJob は埋め込みジョブ行（GORM AutoMigrate で作成）。
type EmbeddingJob struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey"`
	Text      string         `gorm:"type:text"`
	Result    datatypes.JSON `gorm:"type:jsonb"`
	Status    JobStatus      `gorm:"not null;default:pending;index"`
	CreatedAt time.Time      `gorm:"not null;autoCreateTime;index"`
}

// EmbeddingJobImage は、埋め込みジョブに紐づくオブジェクトストレージ上の画像を表す。
type EmbeddingJobImage struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	JobID     uuid.UUID `gorm:"type:uuid;not null;index"`
	ObjectKey string    `gorm:"not null;uniqueIndex"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime;index"`
}

// EmbeddingCache は embedding_caches テーブル（LRU で削除する内部キャッシュ）。
type EmbeddingCache struct {
	Key            string         `gorm:"primaryKey"`
	Value          datatypes.JSON `gorm:"type:jsonb;not null"`
	LastAccessedAt time.Time      `gorm:"not null;autoUpdateTime;index"`
}

// migrate で自動的にテーブル作成されるモデルのリスト。
func Models() []any {
	return []any{
		&EmbeddingJob{},
		&EmbeddingJobImage{},
		&EmbeddingCache{},
	}
}
