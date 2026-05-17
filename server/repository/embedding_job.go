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

	// ErrJobNotFound はジョブIDに対応するジョブが見つからないことを表す。
	ErrJobNotFound = errors.New("job not found")

	// ErrJobFailed はジョブが失敗したことを表す。
	ErrJobFailed = errors.New("job failed")
)

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

type JobState struct {
	Status JobStatus
	Result json.RawMessage
}

type JobRepository interface {
	GetJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error)
	CreateJob(ctx context.Context, id uuid.UUID, payload json.RawMessage) error
	ClaimJob(ctx context.Context) (id uuid.UUID, payload json.RawMessage, err error)
	GetJobState(ctx context.Context, id uuid.UUID) (JobState, error)
	CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	FailJob(ctx context.Context, id uuid.UUID) error
	CountPendingJobs(ctx context.Context) (int, error)
	CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error)
}
