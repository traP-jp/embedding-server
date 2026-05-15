package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
			log.Printf("cache parse text: %v", err)
		} else if !errors.Is(err, repository.ErrEmbeddingCacheNotFound) {
			log.Printf("cache get text: %v", err)
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
			log.Printf("write embedding job: %v", err)
			return api.EmbeddingResult{}, err
		}
		payloadBody.ImagePaths = &imagePaths
	}

	payload, err := json.Marshal(payloadBody)
	if err != nil {
		log.Printf("marshal embedding job: %v", err)
		if err := RemoveJobImageDir(id); err != nil {
			log.Printf("cleanup image job dir id=%s: %v", id, err)
		}
		return api.EmbeddingResult{}, err
	}

	if err := s.repo.CreatePendingJob(ctx, id, payload); err != nil {
		log.Printf("create embedding job: %v", err)
		if err := RemoveJobImageDir(id); err != nil {
			log.Printf("cleanup image job dir id=%s: %v", id, err)
		}
		return api.EmbeddingResult{}, err
	}

	return s.waitEmbeddingResult(ctx, id)
}

func (s *EmbeddingService) waitEmbeddingResult(ctx context.Context, id uuid.UUID) (api.EmbeddingResult, error) {
	// 30s以上workerが時間かかるようならerrorにする
	deadline := time.NewTimer(syncEmbeddingWaitTimeout)
	defer deadline.Stop()

	if s.notifier == nil {
		return api.EmbeddingResult{}, ErrNotifierRequired
	}

	// 結果がすでにできているかもしれないのでdbを見に行く
	if result, err := s.readEmbeddingResult(ctx, id); err == nil {
		return result, nil
	} else if !errors.Is(err, errEmbeddingResultNotReady) {
		return api.EmbeddingResult{}, err
	}

	ch, unsubscribe := s.notifier.Subscribe(id)
	defer unsubscribe()

	// job完了通知がすでに来ているかもしれないのでdbをもう一度見に行く
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
		log.Printf("wait embedding result: %v", err)
		return api.EmbeddingResult{}, err
	}

	switch job.Status {
	case repository.EmbeddingJobStatusFailed:
		return api.EmbeddingResult{}, repository.ErrEmbeddingJobFailed
	case repository.EmbeddingJobStatusPending:
		return api.EmbeddingResult{}, errEmbeddingResultNotReady
	case repository.EmbeddingJobStatusCompleted:
		if len(job.Result) == 0 {
			return api.EmbeddingResult{}, errEmbeddingResultNotReady
		}
	}

	var result api.EmbeddingResult
	if err := json.Unmarshal(job.Result, &result); err != nil {
		log.Printf("parse embedding result: %v", err)
		return api.EmbeddingResult{}, err
	}

	return result, nil
}
