package gormrepo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"embedding-server/api/model"
	"embedding-server/api/repository"
)

func (r *Repository) EmbeddingJobPayload(ctx context.Context, id uuid.UUID) (json.RawMessage, error) {
	var job model.EmbeddingJob
	err := r.db.WithContext(ctx).Select("payload").Where("id = ?", id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrEmbeddingJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(job.Payload), nil
}

func (r *Repository) CreatePendingJob(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
	job := model.EmbeddingJob{
		ID:      id,
		Payload: datatypes.JSON(payload),
		Status:  repository.EmbeddingJobStatusPending,
	}
	return r.db.WithContext(ctx).Create(&job).Error
}

func (r *Repository) ClaimNext(ctx context.Context) (id uuid.UUID, payload json.RawMessage, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.EmbeddingJob
	const q = `
UPDATE embedding_jobs AS j
SET status = ?, started_at = NOW()
WHERE j.id = (
	SELECT id FROM embedding_jobs
	WHERE status = ? AND started_at IS NULL
	ORDER BY created_at ASC, id ASC
	FOR UPDATE SKIP LOCKED
	LIMIT 1
)
AND j.status = ?
AND j.started_at IS NULL
RETURNING id, payload, status, created_at, started_at, completed_at`
		res := tx.Raw(q,
			repository.EmbeddingJobStatusProcessing,
			repository.EmbeddingJobStatusPending,
			repository.EmbeddingJobStatusPending,
		).Scan(&job)
		if res.Error != nil {
			return res.Error
		}
		if job.ID == uuid.Nil {
			return repository.ErrNoJob
		}
		id = job.ID
		payload = json.RawMessage(job.Payload)
		return nil
	})
	if errors.Is(err, repository.ErrNoJob) {
		return uuid.Nil, nil, repository.ErrNoJob
	}
	return id, payload, err
}

func (r *Repository) EmbeddingJobResult(ctx context.Context, id uuid.UUID) (repository.EmbeddingJobState, error) {
	var job model.EmbeddingJob
	err := r.db.WithContext(ctx).Select("status", "result").Where("id = ?", id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.EmbeddingJobState{}, repository.ErrEmbeddingJobNotFound
	}
	if err != nil {
		return repository.EmbeddingJobState{}, err
	}
	return repository.EmbeddingJobState{
		Status: job.Status,
		Result: json.RawMessage(job.Result),
	}, nil
}

func (r *Repository) Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error {
	updates := map[string]any{
		"status": repository.EmbeddingJobStatusCompleted,
	}
	if len(result) > 0 {
		updates["result"] = datatypes.JSON(result)
	}
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, repository.EmbeddingJobStatusProcessing).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}

func (r *Repository) Fail(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, repository.EmbeddingJobStatusProcessing).
		Updates(map[string]any{
			"status": repository.EmbeddingJobStatusFailed,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}

func (r *Repository) CountPendingJobs(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.EmbeddingJob{}).
		Where("status = ?", repository.EmbeddingJobStatusPending).
		Count(&count).Error
	return int(count), err
}

func (r *Repository) DeleteJob(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&model.EmbeddingJob{}, id).Error
}
