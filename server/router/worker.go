package router

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"embedding-server/api/api"
	"embedding-server/api/repository"
	"embedding-server/api/service"
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

	return api.ClaimWorkerJob200JSONResponse{
		Id:      id,
		Payload: payload,
	}, nil
}

// CompleteWorkerJob はジョブ成功完了を記録する。
// JSON ボディに `result` がある場合、テキスト埋め込みジョブはサーバーが内部キャッシュへ保存する。
func (h *Handlers) CompleteWorkerJob(ctx context.Context, req api.CompleteWorkerJobRequestObject) (api.CompleteWorkerJobResponseObject, error) {
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

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("complete invalid payload: %v", err)
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	// テキスト埋め込みジョブの結果はキャッシュする。
	if payload.Text != nil && strings.TrimSpace(*payload.Text) != "" && (payload.ImagePaths == nil || len(*payload.ImagePaths) == 0) {
		if err := h.repo.TextEmbeddingCacheSet(ctx, *payload.Text, resultRaw); err != nil {
			log.Printf("complete cache set: %v", err)
			return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
		}
	}

	if err := service.RemoveJobImageDir(req.Id); err != nil {
		log.Printf("complete cleanup image dir id=%s: %v", req.Id, err)
	}

	return api.CompleteWorkerJob204Response{}, nil
}

// FailWorkerJob はジョブ失敗を記録する。
func (h *Handlers) FailWorkerJob(ctx context.Context, req api.FailWorkerJobRequestObject) (api.FailWorkerJobResponseObject, error) {

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

	if err := service.RemoveJobImageDir(req.Id); err != nil {
		log.Printf("fail cleanup image dir id=%s: %v", req.Id, err)
	}

	return api.FailWorkerJob204Response{}, nil
}
