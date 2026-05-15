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
	ErrTextRequired     = errors.New("text required")
	ErrEmbeddingTimeout = errors.New("embedding timed out")
	ErrNotifierRequired = errors.New("job notifier required")
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

func (s *EmbeddingService) CreateTextEmbedding(ctx context.Context, text string) (api.EmbeddingResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return api.EmbeddingResult{}, ErrTextRequired
	}

	// 先にキャッシュを確認して、あればすぐ返す。なければジョブを作成する。
	if raw, err := s.repo.TextEmbeddingCacheGet(ctx, text); err == nil {
		result, err := parseEmbeddingResult(raw)
		if err == nil {
			return result, nil
		}
		log.Printf("cache parse text: %v", err)
	} else if !errors.Is(err, repository.ErrEmbeddingCacheNotFound) {
		log.Printf("cache get text: %v", err)
		return api.EmbeddingResult{}, err
	}

	payload, err := json.Marshal(map[string]string{
		"kind": "text",
		"text": text,
	})
	if err != nil {
		log.Printf("marshal text job: %v", err)
		return api.EmbeddingResult{}, err
	}

	// dbにjobを追加
	id, err := s.repo.CreatePendingJob(ctx, payload)
	if err != nil {
		log.Printf("create text job: %v", err)
		return api.EmbeddingResult{}, err
	}

	return s.waitEmbeddingResult(ctx, id)
}

func (s *EmbeddingService) CreateImageEmbedding(ctx context.Context, images [][]byte) (api.EmbeddingResult, error) {
	id := uuid.New()
	imagePaths, err := writeJobImages(id, images)
	if err != nil {
		log.Printf("write image job: %v", err)
		return api.EmbeddingResult{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"kind":        "image",
		"image_paths": imagePaths,
	})
	if err != nil {
		log.Printf("marshal image job: %v", err)
		_ = removeJobImageDir(id)
		return api.EmbeddingResult{}, err
	}
	if err := s.repo.CreatePendingJobWithID(ctx, id, payload); err != nil {
		log.Printf("create image job: %v", err)
		_ = removeJobImageDir(id)
		return api.EmbeddingResult{}, err
	}

	return s.waitEmbeddingResult(ctx, id)
}

func (s *EmbeddingService) CreateTextImageEmbedding(ctx context.Context, text string, images [][]byte) (api.EmbeddingResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return api.EmbeddingResult{}, ErrTextRequired
	}

	id := uuid.New()
	imagePaths, err := writeJobImages(id, images)
	if err != nil {
		log.Printf("write text_image job: %v", err)
		return api.EmbeddingResult{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"kind":        "text_image",
		"text":        text,
		"image_paths": imagePaths,
	})
	if err != nil {
		log.Printf("marshal text_image job: %v", err)
		_ = removeJobImageDir(id)
		return api.EmbeddingResult{}, err
	}

	if err := s.repo.CreatePendingJobWithID(ctx, id, payload); err != nil {
		log.Printf("create text_image job: %v", err)
		_ = removeJobImageDir(id)
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

	result, err := parseEmbeddingResult(job.Result)
	if err != nil {
		log.Printf("parse embedding result: %v", err)
		return api.EmbeddingResult{}, err
	}

	return result, nil
}

func parseEmbeddingResult(raw json.RawMessage) (api.EmbeddingResult, error) {
	var result api.EmbeddingResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return api.EmbeddingResult{}, err
	}
	return result, nil
}
