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
		Select("id", "text", "webhook_url", "kind").
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
		WebhookURL:      job.WebhookURL,
		Kind:            job.Kind,
		ImageObjectKeys: keys,
	}, nil
}

func (r *Repository) CreateJob(ctx context.Context, input repository.CreateJobInput) error {
	if input.Kind == "" {
		return errors.New("job kind is required")
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[model.EmbeddingJob](tx).Create(ctx, &model.EmbeddingJob{
			ID:         input.ID,
			Text:       input.Text,
			WebhookURL: input.WebhookURL,
			Kind:       input.Kind,
			Status:     model.StatusPending,
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

func (r *Repository) ClaimJob(ctx context.Context, filter repository.ClaimJobFilter) (*repository.JobRecord, error) {
	wantText := filter.Has(model.JobKindText)
	wantImage := filter.Has(model.JobKindImage)
	if !wantText && !wantImage {
		return nil, repository.ErrNoJob
	}

	query := r.db.WithContext(ctx).
		Table("embedding_jobs").
		Select("embedding_jobs.id", "embedding_jobs.text", "embedding_jobs.webhook_url", "embedding_jobs.kind").
		Where("embedding_jobs.status = ?", model.StatusPending)

	switch {
	case wantText && !wantImage:
		query = query.Where("embedding_jobs.kind = ?", model.JobKindText)
	case !wantText && wantImage:
		query = query.Where("embedding_jobs.kind = ?", model.JobKindImage)
	}

	order := "embedding_jobs.created_at ASC, embedding_jobs.id ASC"
	if wantText && wantImage {
		// text を優先し、同種内は FIFO。
		// modal にはtextを 優先して処理させる
		order = "CASE embedding_jobs.kind WHEN 'text' THEN 0 WHEN 'image' THEN 1 ELSE 2 END ASC, " + order
	}
	query = query.Order(order)

	var job model.EmbeddingJob
	err := query.Limit(1).Take(&job).Error
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
		WebhookURL:      job.WebhookURL,
		Kind:            job.Kind,
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

func (r *Repository) CountPendingTextJobs(ctx context.Context) (int, error) {
	count, err := gorm.G[model.EmbeddingJob](r.db).
		Where("status = ? AND kind = ?", model.StatusPending, model.JobKindText).
		Count(ctx, "id")
	return int(count), err
}

func (r *Repository) CountPendingImageJobs(ctx context.Context) (int, error) {
	count, err := gorm.G[model.EmbeddingJob](r.db).
		Where("status = ? AND kind = ?", model.StatusPending, model.JobKindImage).
		Count(ctx, "id")
	return int(count), err
}

func (r *Repository) CountProcessingImageJobs(ctx context.Context) (int, error) {
	count, err := gorm.G[model.EmbeddingJob](r.db).
		Where("status = ? AND kind = ?", model.StatusProcessing, model.JobKindImage).
		Count(ctx, "id")
	return int(count), err
}

// ReclaimStaleProcessingJobs は更新が古い processing ジョブを pending に戻す。
// Modal timeout / crash 後に claim 済みのまま残ったジョブを再処理可能にする。
func (r *Repository) ReclaimStaleProcessingJobs(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	n, err := gorm.G[model.EmbeddingJob](r.db).
		Where("status = ? AND updated_at < ?", model.StatusProcessing, cutoff).
		Updates(ctx, model.EmbeddingJob{
			Status: model.StatusPending,
		})
	return int64(n), err
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
