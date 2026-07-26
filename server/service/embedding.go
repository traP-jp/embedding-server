package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/model"
	"embedding-server/api/repository"

	"github.com/google/uuid"
)

const syncEmbeddingWaitTimeout = 500 * time.Second

// テキスト同期は部室 worker の待ち行列を塞ぐので、pending の text ジョブが多すぎたら受付拒否する。
// 画像ジョブは非同期＋Modal バッチ前提のため、ここでは上限を掛けない。
const maxPendingTextJobs = 30

var (
	ErrEmbeddingInputRequired = errors.New("embedding input required")
	ErrEmbeddingJobsFull      = errors.New("too many pending embedding jobs")
	ErrEmbeddingTimeout       = errors.New("embedding timed out")
)

var (
	errEmbeddingResultNotReady = errors.New("embedding result not ready")
)

type EmbeddingService struct {
	repo     repository.Repository
	notifier JobNotifier
	jobFile  *JobFileService
	webhook  *WebhookDispatcher
	modal    *ModalTrigger
}

func NewEmbeddingService(
	repo repository.Repository,
	notifier JobNotifier,
	jobFile *JobFileService,
	webhook *WebhookDispatcher,
	modal *ModalTrigger,
) *EmbeddingService {
	return &EmbeddingService{
		repo:     repo,
		notifier: notifier,
		jobFile:  jobFile,
		webhook:  webhook,
		modal:    modal,
	}
}

// CreateEmbedding はテキスト同期用。ジョブ作成後に完了まで待つ。
func (s *EmbeddingService) CreateEmbedding(ctx context.Context, input EmbeddingInput) (api.EmbeddingResult, error) {
	textOnly := input.Text != "" && len(input.Images) == 0
	if textOnly {
		raw, err := s.repo.GetTextCache(ctx, input.Text)
		if err == nil {
			var result api.EmbeddingResult
			if err := json.Unmarshal(raw, &result); err == nil {
				slog.Info("embedding cache hit", slog.Int("text_chars", len(input.Text)), slog.Int("vector_dim", len(result.Vector)))
				return result, nil
			}
			slog.Error("cache parse text", slog.Int("text_chars", len(input.Text)), slog.Any("error", err))
		} else if !errors.Is(err, repository.ErrCacheNotFound) {
			slog.Error("cache get text", slog.Int("text_chars", len(input.Text)), slog.Any("error", err))
			return api.EmbeddingResult{}, err
		} else {
			slog.Debug("embedding cache miss", slog.Int("text_chars", len(input.Text)))
		}

		count, err := s.repo.CountPendingTextJobs(ctx)
		if err != nil {
			slog.Error("count pending text jobs", slog.Any("error", err))
			return api.EmbeddingResult{}, err
		}
		if count >= maxPendingTextJobs {
			slog.Warn("embedding create rejected", slog.String("reason", "text_jobs_full"), slog.Int("pending_text_jobs", count))
			return api.EmbeddingResult{}, ErrEmbeddingJobsFull
		}
	}

	id, err := s.enqueueJob(ctx, input)
	if err != nil {
		return api.EmbeddingResult{}, err
	}
	return s.waitEmbeddingResult(ctx, id)
}

// CreateAsyncEmbedding は画像系非同期用。job id だけ返す。
func (s *EmbeddingService) CreateAsyncEmbedding(ctx context.Context, input EmbeddingInput) (uuid.UUID, error) {
	id, err := s.enqueueJob(ctx, input)
	if err != nil {
		return uuid.Nil, err
	}
	if len(input.Images) > 0 && s.modal != nil {
		s.modal.MaybeTrigger(ctx)
	}
	return id, nil
}

func (s *EmbeddingService) GetJobStatus(ctx context.Context, id uuid.UUID) (api.EmbeddingJobStatus, error) {
	state, err := s.repo.GetJobState(ctx, id)
	if errors.Is(err, repository.ErrJobNotFound) {
		return api.EmbeddingJobStatus{}, repository.ErrJobNotFound
	}
	if err != nil {
		return api.EmbeddingJobStatus{}, err
	}

	out := api.EmbeddingJobStatus{
		Id:     id,
		Status: api.EmbeddingJobStatusStatus(state.Status),
	}
	if state.Status == model.StatusCompleted && len(state.Result) > 0 {
		var result api.EmbeddingResult
		if err := json.Unmarshal(state.Result, &result); err != nil {
			return api.EmbeddingJobStatus{}, err
		}
		out.Result = &result
	}
	return out, nil
}

func (s *EmbeddingService) NotifyWebhookCompleted(ctx context.Context, job *repository.JobRecord, result api.EmbeddingResult) {
	if s.webhook == nil || job == nil {
		return
	}
	s.webhook.Notify(ctx, job.WebhookURL, WebhookPayload{
		ID:     job.ID,
		Status: model.StatusCompleted,
		Result: &result,
	})
}

func (s *EmbeddingService) NotifyWebhookFailed(ctx context.Context, job *repository.JobRecord) {
	if s.webhook == nil || job == nil {
		return
	}
	s.webhook.Notify(ctx, job.WebhookURL, WebhookPayload{
		ID:     job.ID,
		Status: model.StatusFailed,
		Error:  "job failed",
	})
}

func (s *EmbeddingService) enqueueJob(ctx context.Context, input EmbeddingInput) (uuid.UUID, error) {
	if input.Text == "" && len(input.Images) == 0 {
		slog.Warn("embedding create rejected", slog.String("reason", "empty_input"))
		return uuid.Nil, ErrEmbeddingInputRequired
	}

	id := uuid.New()
	imageObjectKeys, err := s.jobFile.StoreJobImages(ctx, id, input.Images)
	if err != nil {
		slog.Error("write embedding job images", slog.String("job_id", id.String()), slog.Int("image_count", len(input.Images)), slog.Any("error", err))
		return uuid.Nil, err
	}

	if err := s.repo.CreateJob(ctx, repository.CreateJobInput{
		ID:              id,
		Text:            input.Text,
		WebhookURL:      input.WebhookURL,
		Kind:            model.JobKindFromHasImages(len(input.Images) > 0),
		ImageObjectKeys: imageObjectKeys,
	}); err != nil {
		slog.Error("create embedding job", slog.String("job_id", id.String()), slog.Any("error", err))
		if remErr := s.jobFile.RemoveJobImages(ctx, imageObjectKeys); remErr != nil {
			slog.Error("cleanup image job dir", slog.String("job_id", id.String()), slog.Any("error", remErr))
		}
		return uuid.Nil, err
	}
	return id, nil
}

func (s *EmbeddingService) waitEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	deadline := time.NewTimer(syncEmbeddingWaitTimeout)
	defer deadline.Stop()

	ch, unsubscribe := s.notifier.Subscribe(id)
	defer unsubscribe()

	if result, err := s.readEmbeddingResult(ctx, id); err == nil {
		slog.Info("embedding wait completed immediately", slog.String("job_id", id.String()), slog.Int("vector_dim", len(result.Vector)))
		return result, nil
	} else if !errors.Is(err, errEmbeddingResultNotReady) {
		slog.Error("embedding wait initial read failed", slog.String("job_id", id.String()), slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}

	select {
	case <-ctx.Done():
		slog.Warn("embedding wait context done", slog.String("job_id", id.String()), slog.Any("error", ctx.Err()))
		return api.EmbeddingResult{}, ctx.Err()
	case <-deadline.C:
		slog.Warn("embedding wait timed out", slog.String("job_id", id.String()), slog.Duration("timeout", syncEmbeddingWaitTimeout))
		return api.EmbeddingResult{}, ErrEmbeddingTimeout
	case <-ch:
		return s.readEmbeddingResult(ctx, id)
	}
}

func (s *EmbeddingService) readEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	job, err := s.repo.GetJobState(ctx, id)
	if errors.Is(err, repository.ErrJobNotFound) {
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	}
	if err != nil {
		slog.Error("wait embedding result", slog.String("job_id", id.String()), slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}

	switch job.Status {
	case model.StatusFailed:
		slog.Warn("embedding result failed", slog.String("job_id", id.String()))
		return api.EmbeddingResult{}, repository.ErrJobFailed
	case model.StatusPending, model.StatusProcessing:
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	case model.StatusCompleted:
	}

	var result api.EmbeddingResult
	if err := json.Unmarshal(job.Result, &result); err != nil {
		slog.Error("parse embedding result", slog.String("job_id", id.String()), slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}
	return result, nil
}
