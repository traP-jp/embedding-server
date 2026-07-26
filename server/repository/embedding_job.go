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
	WebhookURL      string
	Kind            model.JobKind
	ImageObjectKeys []string
}

type JobRecord struct {
	ID              uuid.UUID
	Text            string
	WebhookURL      string
	Kind            model.JobKind
	ImageObjectKeys []string
}

// ClaimJobFilter は claim 時のジョブ種別フィルタ。
// Kinds が空なら text と image の両方を対象にし、text を優先する。
type ClaimJobFilter struct {
	Kinds []model.JobKind
}

// Has は指定 kind を対象にするか。Kinds が空なら全 kind を対象にする。
func (f ClaimJobFilter) Has(kind model.JobKind) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

type JobRepository interface {
	GetJob(ctx context.Context, id uuid.UUID) (*JobRecord, error)
	CreateJob(ctx context.Context, input CreateJobInput) error
	ClaimJob(ctx context.Context, filter ClaimJobFilter) (*JobRecord, error)
	GetJobState(ctx context.Context, id uuid.UUID) (JobState, error)
	CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	FailJob(ctx context.Context, id uuid.UUID) error
	CountPendingTextJobs(ctx context.Context) (int, error)
	CountPendingImageJobs(ctx context.Context) (int, error)
	CountProcessingImageJobs(ctx context.Context) (int, error)
	ReclaimStaleProcessingJobs(ctx context.Context, ttl time.Duration) (int64, error)
	ExpiredJobImageKeys(ctx context.Context, ttl time.Duration) ([]string, error)
	CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error)
}
