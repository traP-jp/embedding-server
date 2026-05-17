package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"embedding-server/api/model"
	"embedding-server/api/repository"
)

func (r *Repository) GetJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error) {
	job, err := gorm.G[model.EmbeddingJob](r.db).
		Select("payload").
		Where("id = ?", id).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(job.Payload), nil
}

func (r *Repository) CreateJob(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
	return gorm.G[model.EmbeddingJob](r.db).Create(ctx, &model.EmbeddingJob{
		ID:      id,
		Payload: datatypes.JSON(payload),
		Status:  repository.StatusPending,
	})
}

func (r *Repository) ClaimJob(ctx context.Context) (uuid.UUID, json.RawMessage, error) {
	job, err := gorm.G[model.EmbeddingJob](r.db).
		Select("id", "payload").
		Where("status = ?", repository.StatusPending).
		Order("created_at ASC, id ASC").
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, nil, repository.ErrNoJob
	}
	if err != nil {
		return uuid.Nil, nil, err
	}

	rowsAffected, err := gorm.G[model.EmbeddingJob](r.db).
		Where("id = ? AND status = ?", job.ID, repository.StatusPending).
		Updates(ctx, model.EmbeddingJob{
			Status: repository.StatusProcessing,
		})
	if err != nil {
		return uuid.Nil, nil, err
	}
	if rowsAffected == 0 {
		return uuid.Nil, nil, repository.ErrNoJob
	}

	return job.ID, json.RawMessage(job.Payload), nil
}

func (r *Repository) GetJobState(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
	job, err := gorm.G[model.EmbeddingJob](r.db).
		Select("status", "result").
		Where("id = ?", id).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.JobState{}, repository.ErrJobNotFound
	}
	if err != nil {
		return repository.JobState{}, err
	}
	return repository.JobState{
		Status: job.Status,
		Result: json.RawMessage(job.Result),
	}, nil
}

func (r *Repository) CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error {
	if len(result) == 0 {
		return errors.New("result is empty")
	}
	rowsAffected, err := gorm.G[model.EmbeddingJob](r.db).
		Where("id = ? AND status = ?", id, repository.StatusProcessing).
		Updates(ctx, model.EmbeddingJob{
			Status: repository.StatusCompleted,
			Result: datatypes.JSON(result),
		})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrJobNotFound
	}
	return nil
}

func (r *Repository) FailJob(ctx context.Context, id uuid.UUID) error {
	rowsAffected, err := gorm.G[model.EmbeddingJob](r.db).
		Where("id = ? AND status = ?", id, repository.StatusProcessing).
		Updates(ctx, model.EmbeddingJob{
			Status: repository.StatusFailed,
		})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrJobNotFound
	}
	return nil
}

func (r *Repository) CountPendingJobs(ctx context.Context) (int, error) {
	count, err := gorm.G[model.EmbeddingJob](r.db).
		Where("status = ?", repository.StatusPending).
		Count(ctx, "id")
	return int(count), err
}

func (r *Repository) CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error) {
	expiredAt := time.Now().Add(-ttl)
	rowsAffected, err := gorm.G[model.EmbeddingJob](r.db).
		Where("created_at < ?", expiredAt).
		Delete(ctx)
	return int64(rowsAffected), err
}
