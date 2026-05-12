package router

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/google/uuid"

	"embedding-server/api/api"
	"embedding-server/api/repository"
)

// ClaimWorkerJob はワーカーが次のジョブを取りに来るエンドポイント。
func (h *Handlers) ClaimWorkerJob(ctx context.Context, _ api.ClaimWorkerJobRequestObject) (api.ClaimWorkerJobResponseObject, error) {
	id, payloadRaw, err := h.repo.ClaimNext(ctx)
	if errors.Is(err, repository.ErrNoJob) {
		return api.ClaimWorkerJob204Response{}, nil
	}
	if err != nil {
		log.Printf("claim: %v", err)
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		log.Printf("claim invalid payload id=%s: %v", id, err)
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	if _, err := payload.ValueByDiscriminator(); err != nil {
		log.Printf("claim invalid payload id=%s: %v", id, err)
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	return api.ClaimWorkerJob200JSONResponse{
		Id:      id,
		Payload: payload,
	}, nil
}

// CompleteWorkerJob はジョブ成功完了を記録する。
// JSON ボディに `result` がある場合、テキスト埋め込みジョブはサーバーが内部キャッシュへ保存する。
func (h *Handlers) CompleteWorkerJob(ctx context.Context, req api.CompleteWorkerJobRequestObject) (api.CompleteWorkerJobResponseObject, error) {
	if req.Id == uuid.Nil {
		return api.CompleteWorkerJob400JSONResponse{Message: "invalid id"}, nil
	}
	if req.Body == nil {
		return api.CompleteWorkerJob400JSONResponse{Message: "invalid json"}, nil
	}

	rawPayload, err := h.repo.EmbeddingJobPayload(ctx, req.Id)
	if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
		return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
	}
	if err != nil {
		log.Printf("complete load payload: %v", err)
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	resultRaw, err := json.Marshal(req.Body.Result)
	if err != nil {
		log.Printf("complete marshal result: %v", err)
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if err := h.repo.Complete(ctx, req.Id, resultRaw); err != nil {
		if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
			return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
		}
		log.Printf("complete: %v", err)
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}

	var p struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rawPayload, &p); err == nil && p.Kind == "text" {
		t := strings.TrimSpace(p.Text)
		if t != "" {
			cacheKey := repository.TextEmbeddingCacheKey(t)
			if err := h.repo.CacheSet(ctx, cacheKey, resultRaw, nil); err != nil {
				log.Printf("complete cache set: %v", err)
			}
		}
	}
	return api.CompleteWorkerJob204Response{}, nil
}

// FailWorkerJob はジョブ失敗を記録する。
func (h *Handlers) FailWorkerJob(ctx context.Context, req api.FailWorkerJobRequestObject) (api.FailWorkerJobResponseObject, error) {
	if req.Id == uuid.Nil {
		return api.FailWorkerJob400JSONResponse{Message: "invalid id"}, nil
	}
	if err := h.repo.Fail(ctx, req.Id); err != nil {
		if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
			return api.FailWorkerJob404JSONResponse{Message: "not found"}, nil
		}
		log.Printf("fail: %v", err)
		return api.FailWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}
	return api.FailWorkerJob204Response{}, nil
}
