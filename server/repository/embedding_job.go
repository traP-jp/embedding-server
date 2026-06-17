//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"embedding-server/api/model"

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

type JobState struct {
	Status model.JobStatus
	Result json.RawMessage
}

type CreateJobInput struct {
	ID              uuid.UUID
	Text            string
	ImageObjectKeys []string
}

type JobRecord struct {
	ID              uuid.UUID
	Text            string
	ImageObjectKeys []string
}

type JobRepository interface {
	GetJob(ctx context.Context, id uuid.UUID) (*JobRecord, error)
	CreateJob(ctx context.Context, input CreateJobInput) error
	ClaimJob(ctx context.Context) (*JobRecord, error)
	GetJobState(ctx context.Context, id uuid.UUID) (JobState, error)
	CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	FailJob(ctx context.Context, id uuid.UUID) error
	CountPendingJobs(ctx context.Context) (int, error)
	ExpiredJobImageKeys(ctx context.Context, ttl time.Duration) ([]string, error)
	CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error)
}
