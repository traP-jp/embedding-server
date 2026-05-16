package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"

	"github.com/google/uuid"
)

const syncEmbeddingWaitTimeout = 30 * time.Second

// workerが30s timeoutで処理するため、30件以上pendingがあれば受付を止める。
const maxPendingEmbeddingJobs = 30

var (
	ErrEmbeddingInputRequired = errors.New("embedding input required")
	ErrEmbeddingJobsFull      = errors.New("too many pending embedding jobs")
	ErrEmbeddingTimeout       = errors.New("embedding timed out")
	ErrNotifierRequired       = errors.New("job notifier required")
)

var (
	errEmbeddingResultNotReady = errors.New("embedding result not ready")
)

type EmbeddingService struct {
	repo     repository.Repository
	notifier JobNotifier
	jobFile  *JobFileService
}

func NewEmbeddingService(repo repository.Repository, notifier JobNotifier, jobFile *JobFileService) *EmbeddingService {
	return &EmbeddingService{repo: repo, notifier: notifier, jobFile: jobFile}
}

// routerから呼ばれる関数。embeddingジョブを作成し、完了するまで待機する。
func (s *EmbeddingService) CreateEmbedding(ctx context.Context, input EmbeddingInput) (api.EmbeddingResult, error) {
	if input.Text == "" && len(input.Images) == 0 {
		return api.EmbeddingResult{}, ErrEmbeddingInputRequired
	}

	if input.Text != "" && len(input.Images) == 0 { // textのみの場合にキャッシュを確認する
		raw, err := s.repo.TextEmbeddingCacheGet(ctx, input.Text)
		if err == nil {
			var result api.EmbeddingResult
			if err := json.Unmarshal(raw, &result); err == nil {
				return result, nil
			}
			slog.Error("cache parse text", slog.Any("error", err))
		} else if !errors.Is(err, repository.ErrEmbeddingCacheNotFound) { // キャッシュがない以外のエラーはログに出す
			slog.Error("cache get text", slog.Any("error", err))
			return api.EmbeddingResult{}, err
		}
	}

	count, err := s.repo.CountPendingJobs(ctx)
	if err != nil {
		slog.Error("count pending jobs", slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}
	if count >= maxPendingEmbeddingJobs {
		return api.EmbeddingResult{}, ErrEmbeddingJobsFull
	}

	payloadBody := api.WorkerJobPayload{}
	if input.Text != "" {
		payloadBody.Text = &input.Text
	}

	id := uuid.New()
	if len(input.Images) > 0 {
		imagePaths, err := s.jobFile.WriteJobImages(id, input.Images)
		if err != nil {
			slog.Error("write embedding job", slog.Any("error", err))
			return api.EmbeddingResult{}, err
		}
		payloadBody.ImagePaths = &imagePaths
	}

	payload, err := json.Marshal(payloadBody)
	if err != nil {
		slog.Error("marshal embedding job", slog.Any("error", err))
		if err := s.jobFile.RemoveJobImageDir(id); err != nil {
			slog.Error("cleanup image job dir", slog.String("job_id", id.String()), slog.Any("error", err))
		}
		return api.EmbeddingResult{}, err
	}

	if err := s.repo.CreatePendingJob(ctx, id, payload); err != nil {
		slog.Error("create embedding job", slog.Any("error", err))
		if err := s.jobFile.RemoveJobImageDir(id); err != nil {
			slog.Error("cleanup image job dir", slog.String("job_id", id.String()), slog.Any("error", err))
		}
		return api.EmbeddingResult{}, err
	}

	return s.waitEmbeddingResult(ctx, id)
}

// jobの終了を待つ。ジョブが完了していれば結果を返し、完了していなければ待機する。
func (s *EmbeddingService) waitEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	if s.notifier == nil {
		return api.EmbeddingResult{}, ErrNotifierRequired
	}

	deadline := time.NewTimer(syncEmbeddingWaitTimeout)
	defer deadline.Stop()

	ch, unsubscribe := s.notifier.Subscribe(id)
	defer unsubscribe()

	if result, err := s.readEmbeddingResult(ctx, id); err == nil {
		return result, nil
	} else if !errors.Is(err, errEmbeddingResultNotReady) {
		return api.EmbeddingResult{}, err
	}

	select {
	case <-ctx.Done():
		return api.EmbeddingResult{}, ctx.Err()
	case <-deadline.C:
		return api.EmbeddingResult{}, ErrEmbeddingTimeout
	case <-ch:
		return s.readEmbeddingResult(ctx, id)
	}

}

// dbのjobの状態がcompletedかどうかを確認する。
func (s *EmbeddingService) readEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	job, err := s.repo.EmbeddingJobResult(ctx, id)
	if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	}
	if err != nil {
		slog.Error("wait embedding result", slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}

	switch job.Status {
	case repository.EmbeddingJobStatusFailed:
		return api.EmbeddingResult{}, repository.ErrEmbeddingJobFailed
	case repository.EmbeddingJobStatusPending:
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	case repository.EmbeddingJobStatusProcessing:
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	case repository.EmbeddingJobStatusCompleted:
	}

	var result api.EmbeddingResult
	if err := json.Unmarshal(job.Result, &result); err != nil {
		slog.Error("parse embedding result", slog.Any("error", err))
		return api.EmbeddingResult{}, err
	}

	return result, nil
}
