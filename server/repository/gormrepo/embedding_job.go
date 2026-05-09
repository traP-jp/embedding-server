package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"embedding-server/api/model"
	"embedding-server/api/repository"
)

func (r *Repository) EmbeddingJobPayload(ctx context.Context, id int64) (json.RawMessage, error) {
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

func (r *Repository) CreatePendingJob(ctx context.Context, payload json.RawMessage) (int64, error) {
	job := model.EmbeddingJob{
		Payload: datatypes.JSON(payload),
		Status:  "pending",
	}
	if err := r.db.WithContext(ctx).Create(&job).Error; err != nil {
		return 0, err
	}
	return job.ID, nil
}

func (r *Repository) CreateJobWithStatus(ctx context.Context, payload json.RawMessage, status string) (int64, error) {
	job := model.EmbeddingJob{
		Payload: datatypes.JSON(payload),
		Status:  status,
	}
	if err := r.db.WithContext(ctx).Create(&job).Error; err != nil {
		return 0, err
	}
	return job.ID, nil
}

func (r *Repository) UpdatePayloadAndStatus(ctx context.Context, id int64, fromStatus string, toStatus string, payload json.RawMessage) error {
	updates := map[string]any{
		"payload": datatypes.JSON(payload),
		"status":  toStatus,
	}
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}

func (r *Repository) FailUpload(ctx context.Context, id int64) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, "uploading").
		Updates(map[string]any{
			"status":       "failed",
			"completed_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}

func (r *Repository) ClaimNext(ctx context.Context) (id int64, payload json.RawMessage, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.EmbeddingJob
		const q = `
UPDATE embedding_jobs
SET status = ?, started_at = CURRENT_TIMESTAMP
WHERE id = (
	SELECT id FROM embedding_jobs
	WHERE status = ?
	ORDER BY id ASC
	LIMIT 1
)
AND status = ?
RETURNING id, payload, status, created_at, started_at, completed_at`
		res := tx.Raw(q, "processing", "pending", "pending").Scan(&job)
		if res.Error != nil {
			return res.Error
		}
		if job.ID == 0 {
			return repository.ErrNoJob
		}
		id = job.ID
		payload = json.RawMessage(job.Payload)
		return nil
	})
	if errors.Is(err, repository.ErrNoJob) {
		return 0, nil, repository.ErrNoJob
	}
	return id, payload, err
}

func (r *Repository) EmbeddingJobResult(ctx context.Context, id int64) (json.RawMessage, bool, error) {
	var job model.EmbeddingJob
	err := r.db.WithContext(ctx).Select("status", "result").Where("id = ?", id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, repository.ErrEmbeddingJobNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if job.Status != "completed" || len(job.Result) == 0 {
		return nil, false, nil
	}
	return json.RawMessage(job.Result), true, nil
}

func (r *Repository) Complete(ctx context.Context, id int64, result json.RawMessage) error {
	now := time.Now()
	updates := map[string]any{
		"status":       "completed",
		"completed_at": now,
	}
	if len(result) > 0 {
		updates["result"] = datatypes.JSON(result)
	}
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, "processing").
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}

func (r *Repository) Fail(ctx context.Context, id int64) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.EmbeddingJob{}).
		Where("id = ? AND status = ?", id, "processing").
		Updates(map[string]any{
			"status":       "failed",
			"completed_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingJobNotFound
	}
	return nil
}
