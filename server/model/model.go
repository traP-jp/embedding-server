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

// JobKind は埋め込みジョブの種別。
// multimodal（text+画像）も画像処理経路なので image とする。
type JobKind string

const (
	JobKindText  JobKind = "text"
	JobKindImage JobKind = "image"
)

// JobKindFromHasImages は画像の有無から kind を決める。
// multimodal（text+画像）も画像処理経路なので image。
func JobKindFromHasImages(hasImages bool) JobKind {
	if hasImages {
		return JobKindImage
	}
	return JobKindText
}

// EmbeddingJob は埋め込みジョブ行（GORM AutoMigrate で作成）。
type EmbeddingJob struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey"`
	Text       string         `gorm:"type:text"`
	WebhookURL string         `gorm:"type:text"`
	Result     datatypes.JSON `gorm:"type:jsonb"`
	Kind       JobKind        `gorm:"type:text;not null;index:idx_embedding_jobs_status_kind_created,priority:2"`
	// status+kind+created_at は claim 用、status+updated_at は stale reclaim 用。
	Status    JobStatus `gorm:"not null;default:pending;index:idx_embedding_jobs_status_kind_created,priority:1;index:idx_embedding_jobs_status_updated,priority:1"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime;index:idx_embedding_jobs_status_kind_created,priority:3"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime;index:idx_embedding_jobs_status_updated,priority:2"`
}

// EmbeddingJobImage は、埋め込みジョブに紐づくオブジェクトストレージ上の画像を表す。
type EmbeddingJobImage struct {
	ID        uuid.UUID    `gorm:"type:uuid;primaryKey"`
	JobID     uuid.UUID    `gorm:"type:uuid;not null;index"`
	Job       EmbeddingJob `gorm:"foreignKey:JobID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	ObjectKey string       `gorm:"not null;uniqueIndex"`
	CreatedAt time.Time    `gorm:"not null;autoCreateTime;index"`
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
