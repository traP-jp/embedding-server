//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrNoJob                = errors.New("no job available")
	ErrEmbeddingJobNotFound = errors.New("embedding job not found")
	ErrEmbeddingJobFailed   = errors.New("embedding job failed")
)

type EmbeddingJobStatus string

const (
	EmbeddingJobStatusPending   EmbeddingJobStatus = "pending"
	EmbeddingJobStatusCompleted EmbeddingJobStatus = "completed"
	EmbeddingJobStatusFailed    EmbeddingJobStatus = "failed"
)

type EmbeddingJobState struct {
	Status EmbeddingJobStatus
	Result json.RawMessage
}

type EmbeddingJobRepository interface {
	EmbeddingJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error)
	CreatePendingJob(ctx context.Context, payload json.RawMessage) (uuid.UUID, error)
	CreatePendingJobWithID(ctx context.Context, id uuid.UUID, payload json.RawMessage) error
	ClaimNext(ctx context.Context) (id uuid.UUID, payload json.RawMessage, err error)
	EmbeddingJobResult(ctx context.Context, id uuid.UUID) (EmbeddingJobState, error)
	Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	Fail(ctx context.Context, id uuid.UUID) error
}
