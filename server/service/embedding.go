package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"

	"github.com/google/uuid"
)

const syncEmbeddingWaitTimeout = 30 * time.Second

var (
	ErrEmbeddingInputRequired = errors.New("embedding input required")
	ErrEmbeddingTimeout       = errors.New("embedding timed out")
	ErrNotifierRequired       = errors.New("job notifier required")
)

var (
	errEmbeddingResultNotReady = errors.New("embedding result not ready")
)

type EmbeddingService struct {
	repo     repository.Repository
	notifier JobNotifier
}

func NewEmbeddingService(repo repository.Repository, notifier JobNotifier) *EmbeddingService {
	return &EmbeddingService{repo: repo, notifier: notifier}
}

func (s *EmbeddingService) CreateEmbedding(ctx context.Context, text string, images [][]byte) (api.EmbeddingResult, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 {
		return api.EmbeddingResult{}, ErrEmbeddingInputRequired
	}

	if len(images) == 0 {
		// テキストのみの場合は、先にキャッシュを確認して、あればすぐ返す
		if raw, err := s.repo.TextEmbeddingCacheGet(ctx, text); err == nil {
			var result api.EmbeddingResult
			if err := json.Unmarshal(raw, &result); err == nil {
				return result, nil
			}
			slog.Error("cache parse text", slog.Any("error", err))
		} else if !errors.Is(err, repository.ErrEmbeddingCacheNotFound) {
			slog.Error("cache get text", slog.Any("error", err))
			return api.EmbeddingResult{}, err
		}
	}

	payloadBody := api.WorkerJobPayload{}
	if text != "" {
		payloadBody.Text = &text
	}

	id := uuid.New()
	if len(images) > 0 {
		imagePaths, err := writeJobImages(id, images)
		if err != nil {
			slog.Error("write embedding job", slog.Any("error", err))
			return api.EmbeddingResult{}, err
		}
		payloadBody.ImagePaths = &imagePaths
	}

	payload, err := json.Marshal(payloadBody)
	if err != nil {
		slog.Error("marshal embedding job", slog.Any("error", err))
		if err := RemoveJobImageDir(id); err != nil {
			slog.Error("cleanup image job dir", slog.String("job_id", id.String()), slog.Any("error", err))
		}
		return api.EmbeddingResult{}, err
	}

	if err := s.repo.CreatePendingJob(ctx, id, payload); err != nil {
		slog.Error("create embedding job", slog.Any("error", err))
		if err := RemoveJobImageDir(id); err != nil {
			slog.Error("cleanup image job dir", slog.String("job_id", id.String()), slog.Any("error", err))
		}
		return api.EmbeddingResult{}, err
	}

	return s.waitEmbeddingResult(ctx, id)
}

// jobの終了を待つ。ジョブが完了していれば結果を返し、完了していなければ待機する。
func (s *EmbeddingService) waitEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	deadline := time.NewTimer(syncEmbeddingWaitTimeout)
	defer deadline.Stop()

	if s.notifier == nil {
		return api.EmbeddingResult{}, ErrNotifierRequired
	}

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
