//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrNoJob                = errors.New("no job available")
	ErrEmbeddingJobNotFound = errors.New("embedding job not found")
)

type EmbeddingJobRepository interface {
	EmbeddingJobPayload(ctx context.Context, id int64) (json.RawMessage, error)
	CreatePendingJob(ctx context.Context, payload json.RawMessage) (int64, error)
	CreateJobWithStatus(ctx context.Context, payload json.RawMessage, status string) (int64, error)
	UpdatePayloadAndStatus(ctx context.Context, id int64, fromStatus string, toStatus string, payload json.RawMessage) error
	FailUpload(ctx context.Context, id int64) error
	ClaimNext(ctx context.Context) (id int64, payload json.RawMessage, err error)
	EmbeddingJobResult(ctx context.Context, id int64) (json.RawMessage, bool, error)
	Complete(ctx context.Context, id int64, result json.RawMessage) error
	Fail(ctx context.Context, id int64) error
}
