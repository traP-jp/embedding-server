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

func (r *Repository) GetJob(ctx context.Context, id uuid.UUID) (*repository.JobRecord, error) {
	job, err := gorm.G[model.EmbeddingJob](r.db).
		Select("id", "text").
		Where("id = ?", id).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}

	keys, err := r.jobImageKeys(ctx, id)
	if err != nil {
		return nil, err
	}
	return &repository.JobRecord{
		ID:              job.ID,
		Text:            job.Text,
		ImageObjectKeys: keys,
	}, nil
}

func (r *Repository) CreateJob(ctx context.Context, input repository.CreateJobInput) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[model.EmbeddingJob](tx).Create(ctx, &model.EmbeddingJob{
			ID:     input.ID,
			Text:   input.Text,
			Status: model.StatusPending,
		}); err != nil {
			return err
		}

		if len(input.ImageObjectKeys) == 0 {
			return nil
		}

		images := make([]model.EmbeddingJobImage, 0, len(input.ImageObjectKeys))
		for _, key := range input.ImageObjectKeys {
			images = append(images, model.EmbeddingJobImage{
				ID:        uuid.New(),
				JobID:     input.ID,
				ObjectKey: key,
			})
		}
		return gorm.G[model.EmbeddingJobImage](tx).CreateInBatches(ctx, &images, len(images))
	})
}

func (r *Repository) ClaimJob(ctx context.Context) (*repository.JobRecord, error) {
	job, err := gorm.G[model.EmbeddingJob](r.db).
		Select("id", "text").
		Where("status = ?", model.StatusPending).
		Order("created_at ASC, id ASC").
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNoJob
	}
	if err != nil {
		return nil, err
	}

	rowsAffected, err := gorm.G[model.EmbeddingJob](r.db).
		Where("id = ? AND status = ?", job.ID, model.StatusPending).
		Updates(ctx, model.EmbeddingJob{
			Status: model.StatusProcessing,
		})
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, repository.ErrNoJob
	}

	keys, err := r.jobImageKeys(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	return &repository.JobRecord{
		ID:              job.ID,
		Text:            job.Text,
		ImageObjectKeys: keys,
	}, nil
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
		Where("id = ? AND status = ?", id, model.StatusProcessing).
		Updates(ctx, model.EmbeddingJob{
			Status: model.StatusCompleted,
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
		Where("id = ? AND status = ?", id, model.StatusProcessing).
		Updates(ctx, model.EmbeddingJob{
			Status: model.StatusFailed,
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
		Where("status = ?", model.StatusPending).
		Count(ctx, "id")
	return int(count), err
}

func (r *Repository) ExpiredJobImageKeys(ctx context.Context, ttl time.Duration) ([]string, error) {
	expiredAt := time.Now().Add(-ttl)
	return gorm.G[string](r.db).
		Table("embedding_job_images").
		Select("object_key").
		Where("created_at < ?", expiredAt).
		Find(ctx)
}

func (r *Repository) CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error) {
	expiredAt := time.Now().Add(-ttl)
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := gorm.G[uuid.UUID](tx).
			Table("embedding_jobs").
			Select("id").
			Where("created_at < ?", expiredAt).
			Find(ctx)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		if _, err := gorm.G[model.EmbeddingJobImage](tx).
			Where("job_id IN ?", ids).
			Delete(ctx); err != nil {
			return err
		}
		rowsAffected, err := gorm.G[model.EmbeddingJob](tx).
			Where("id IN ?", ids).
			Delete(ctx)
		if err != nil {
			return err
		}
		deleted = int64(rowsAffected)
		return nil
	})
	return deleted, err
}

func (r *Repository) jobImageKeys(ctx context.Context, id uuid.UUID) ([]string, error) {
	return gorm.G[string](r.db).
		Table("embedding_job_images").
		Select("object_key").
		Where("job_id = ?", id).
		Order("created_at ASC, id ASC").
		Find(ctx)
}
