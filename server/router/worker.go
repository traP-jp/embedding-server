package router

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"embedding-server/api/api"
	"embedding-server/api/repository"
)

// ClaimWorkerJob はワーカーが次のジョブを取りに来るエンドポイント。
func (h *Handlers) ClaimWorkerJob(ctx context.Context, _ api.ClaimWorkerJobRequestObject) (api.ClaimWorkerJobResponseObject, error) {
	id, payloadRaw, err := h.repo.ClaimJob(ctx)
	if errors.Is(err, repository.ErrNoJob) {
		return api.ClaimWorkerJob204Response{}, nil
	}
	if err != nil {
		slog.Error("claim", slog.Any("error", err))
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		slog.Error("claim invalid payload", slog.String("job_id", id.String()), slog.Any("error", err))
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
	if req.Body == nil || len(req.Body.Result.Vector) == 0 {
		return api.CompleteWorkerJob400JSONResponse{Message: "result vector required"}, nil
	}

	rawPayload, err := h.repo.GetJobPayload(ctx, req.Id)
	if errors.Is(err, repository.ErrJobNotFound) {
		return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
	}
	if err != nil {
		slog.Error("complete load payload", slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	resultRaw, err := json.Marshal(req.Body.Result)
	if err != nil {
		slog.Error("complete marshal result", slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if err := h.repo.CompleteJob(ctx, req.Id, resultRaw); err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
		}
		slog.Error("complete", slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		slog.Error("complete invalid payload", slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	// テキスト埋め込みジョブの結果はキャッシュする。
	if payload.Text != nil && strings.TrimSpace(*payload.Text) != "" && (payload.ImagePaths == nil || len(*payload.ImagePaths) == 0) {
		if err := h.repo.SetTextCache(ctx, *payload.Text, resultRaw); err != nil {
			slog.Error("complete cache set", slog.Any("error", err))
			return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
		}
	}

	if err := h.jobFile.RemoveJobImageDir(req.Id); err != nil {
		slog.Error("complete cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	return api.CompleteWorkerJob204Response{}, nil
}

// FailWorkerJob はジョブ失敗を記録する。
func (h *Handlers) FailWorkerJob(ctx context.Context, req api.FailWorkerJobRequestObject) (api.FailWorkerJobResponseObject, error) {

	if err := h.repo.FailJob(ctx, req.Id); err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return api.FailWorkerJob404JSONResponse{Message: "not found"}, nil
		}

		slog.Error("fail", slog.Any("error", err))
		return api.FailWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}

	if err := h.jobFile.RemoveJobImageDir(req.Id); err != nil {
		slog.Error("fail cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	return api.FailWorkerJob204Response{}, nil
}
