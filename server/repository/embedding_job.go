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

type EmbeddingJobState struct {
	Status string
	Result json.RawMessage
}

type EmbeddingJobRepository interface {
	EmbeddingJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error)
	CreatePendingJob(ctx context.Context, payload json.RawMessage) (uuid.UUID, error)
	CreateJobWithStatus(ctx context.Context, payload json.RawMessage, status string) (uuid.UUID, error)
	UpdatePayloadAndStatus(ctx context.Context, id uuid.UUID, fromStatus string, toStatus string, payload json.RawMessage) error
	FailUpload(ctx context.Context, id uuid.UUID) error
	ClaimNext(ctx context.Context) (id uuid.UUID, payload json.RawMessage, err error)
	EmbeddingJobResult(ctx context.Context, id uuid.UUID) (EmbeddingJobState, error)
	Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error
	Fail(ctx context.Context, id uuid.UUID) error
}
