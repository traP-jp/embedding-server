//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNoJob はこれ以上処理すべきジョブがないことを表す。
	ErrNoJob = errors.New("no job available")

	// ErrEmbeddingJobNotFound はジョブIDに対応するジョブが見つからないことを表す。
	ErrEmbeddingJobNotFound = errors.New("embedding job not found")

	// ErrEmbeddingJobFailed はジョブが失敗したことを表す。
	ErrEmbeddingJobFailed = errors.New("embedding job failed")
)

type EmbeddingJobStatus string

const (
	EmbeddingJobStatusPending    EmbeddingJobStatus = "pending"
	EmbeddingJobStatusProcessing EmbeddingJobStatus = "processing"
	EmbeddingJobStatusCompleted  EmbeddingJobStatus = "completed"
	EmbeddingJobStatusFailed     EmbeddingJobStatus = "failed"
)

type EmbeddingJobState struct {
	Status EmbeddingJobStatus
	Result json.RawMessage
}

type EmbeddingJobRepository interface {
	EmbeddingJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error)
	CreatePendingJob(ctx context.Context, id uuid.UUID, payload json.RawMessage) error
	ClaimNext(ctx context.Context) (id uuid.UUID, payload json.RawMessage, err error)
	EmbeddingJobResult(ctx context.Context, id uuid.UUID) (EmbeddingJobState, error)
	Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	Fail(ctx context.Context, id uuid.UUID) error
	CountPendingJobs(ctx context.Context) (int, error)
	DeleteJob(ctx context.Context, id uuid.UUID) error
	CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error)
}
