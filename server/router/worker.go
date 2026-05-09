package router

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"embedding-server/api/api"
	"embedding-server/api/repository"
)

// PostInternalWorkerJobsClaim はワーカーが次のジョブを取りに来るエンドポイント。
func (h *Handlers) PostInternalWorkerJobsClaim(ctx context.Context, _ api.PostInternalWorkerJobsClaimRequestObject) (api.PostInternalWorkerJobsClaimResponseObject, error) {
	id, payloadRaw, err := h.Repo.ClaimNext(ctx)
	if errors.Is(err, repository.ErrNoJob) {
		return api.PostInternalWorkerJobsClaim204Response{}, nil
	}
	if err != nil {
		log.Printf("claim: %v", err)
		return api.PostInternalWorkerJobsClaim500JSONResponse{Message: "internal error"}, nil
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		log.Printf("claim invalid payload id=%d: %v", id, err)
		return api.PostInternalWorkerJobsClaim500JSONResponse{Message: "internal error"}, nil
	}
	if _, err := payload.ValueByDiscriminator(); err != nil {
		log.Printf("claim invalid payload id=%d: %v", id, err)
		return api.PostInternalWorkerJobsClaim500JSONResponse{Message: "internal error"}, nil
	}
	return api.PostInternalWorkerJobsClaim200JSONResponse{
		Id:      id,
		Payload: payload,
	}, nil
}

// PostInternalWorkerJobsIdComplete はジョブ成功完了を記録する。
// JSON ボディに `result` がある場合、テキスト埋め込みジョブはサーバーが内部キャッシュへ保存する。
func (h *Handlers) PostInternalWorkerJobsIdComplete(ctx context.Context, req api.PostInternalWorkerJobsIdCompleteRequestObject) (api.PostInternalWorkerJobsIdCompleteResponseObject, error) {
	if req.Id < 1 {
		return api.PostInternalWorkerJobsIdComplete400JSONResponse{Message: "invalid id"}, nil
	}
	if req.Body == nil {
		return api.PostInternalWorkerJobsIdComplete400JSONResponse{Message: "invalid json"}, nil
	}

	rawPayload, err := h.Repo.EmbeddingJobPayload(ctx, int64(req.Id))
	if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
		return api.PostInternalWorkerJobsIdComplete404JSONResponse{Message: "not found"}, nil
	}
	if err != nil {
		log.Printf("complete load payload: %v", err)
		return api.PostInternalWorkerJobsIdComplete500JSONResponse{Message: "internal error"}, nil
	}

	resultRaw, err := json.Marshal(req.Body.Result)
	if err != nil {
		log.Printf("complete marshal result: %v", err)
		return api.PostInternalWorkerJobsIdComplete204Response{}, nil
	}

	if err := h.Repo.Complete(ctx, int64(req.Id), resultRaw); err != nil {
		if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
			return api.PostInternalWorkerJobsIdComplete404JSONResponse{Message: "not found"}, nil
		}
		log.Printf("complete: %v", err)
		return api.PostInternalWorkerJobsIdComplete500JSONResponse{Message: "internal error"}, nil
	}

	var p struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rawPayload, &p); err == nil && p.Kind == "text" {
		t := strings.TrimSpace(p.Text)
		if t != "" {
			cacheKey := repository.TextEmbeddingCacheKey(t)
			if err := h.Repo.CacheSet(ctx, cacheKey, resultRaw, nil); err != nil {
				log.Printf("complete cache set: %v", err)
			}
		}
	}
	return api.PostInternalWorkerJobsIdComplete204Response{}, nil
}

// PostInternalWorkerJobsIdFail はジョブ失敗を記録する。
func (h *Handlers) PostInternalWorkerJobsIdFail(ctx context.Context, req api.PostInternalWorkerJobsIdFailRequestObject) (api.PostInternalWorkerJobsIdFailResponseObject, error) {
	if req.Id < 1 {
		return api.PostInternalWorkerJobsIdFail400JSONResponse{Message: "invalid id"}, nil
	}
	if err := h.Repo.Fail(ctx, int64(req.Id)); err != nil {
		if errors.Is(err, repository.ErrEmbeddingJobNotFound) {
			return api.PostInternalWorkerJobsIdFail404JSONResponse{Message: "not found"}, nil
		}
		log.Printf("fail: %v", err)
		return api.PostInternalWorkerJobsIdFail500JSONResponse{Message: "internal error"}, nil
	}
	return api.PostInternalWorkerJobsIdFail204Response{}, nil
}
