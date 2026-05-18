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

// ClaimWorkerJobは、ワーカーが次に利用可能なジョブを取得するエンドポイントである。
// 保留中のジョブがない場合は204を返す。
func (h *Handlers) ClaimWorkerJob(ctx context.Context, _ api.ClaimWorkerJobRequestObject) (api.ClaimWorkerJobResponseObject, error) {
	id, payloadRaw, err := h.repo.ClaimJob(ctx)
	if errors.Is(err, repository.ErrNoJob) {
		return api.ClaimWorkerJob204Response{}, nil
	}
	if err != nil {
		slog.Error("claim worker job", slog.Any("error", err))
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		slog.Error(
			"claim worker job invalid payload",
			slog.String("job_id", id.String()),
			slog.Int("payload_bytes", len(payloadRaw)),
			slog.Any("error", err),
		)
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	return api.ClaimWorkerJob200JSONResponse{
		Id:      id,
		Payload: payload,
	}, nil
}

// CompleteWorkerJobは、ジョブの正常な完了を記録する。
// ジョブペイロードがテキストのみの場合、サーバーはそれを内部にキャッシュする。
func (h *Handlers) CompleteWorkerJob(ctx context.Context, req api.CompleteWorkerJobRequestObject) (api.CompleteWorkerJobResponseObject, error) {
	if req.Body == nil || len(req.Body.Result.Vector) == 0 {
		slog.Warn("worker job complete rejected", slog.String("job_id", req.Id.String()), slog.String("reason", "empty_vector"))
		return api.CompleteWorkerJob400JSONResponse{Message: "result vector required"}, nil
	}

	rawPayload, err := h.repo.GetJobPayload(ctx, req.Id)
	if errors.Is(err, repository.ErrJobNotFound) {
		slog.Warn("worker job complete not found", slog.String("job_id", req.Id.String()))
		return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
	}
	if err != nil {
		slog.Error("complete load payload", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	resultRaw, err := json.Marshal(req.Body.Result)
	if err != nil {
		slog.Error("complete marshal result", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if err := h.repo.CompleteJob(ctx, req.Id, resultRaw); err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			slog.Warn("worker job complete state not found", slog.String("job_id", req.Id.String()))
			return api.CompleteWorkerJob404JSONResponse{Message: "not found"}, nil
		}
		slog.Error("complete worker job", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}
	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}

	var payload api.WorkerJobPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		slog.Error("complete invalid payload", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.CompleteWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	// テキスト埋め込みジョブの結果はキャッシュする。
	if payload.Text != nil && strings.TrimSpace(*payload.Text) != "" && (payload.ImagePaths == nil || len(*payload.ImagePaths) == 0) {
		if err := h.repo.SetTextCache(ctx, *payload.Text, resultRaw); err != nil {
			slog.Warn("complete cache set failed, continuing anyway", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		} else {
			slog.Debug("worker job result cached", slog.String("job_id", req.Id.String()), slog.Int("text_chars", len(*payload.Text)))
		}
	}

	if err := h.jobFile.RemoveJobImageDir(req.Id); err != nil {
		slog.Error("complete cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	return api.CompleteWorkerJob204Response{}, nil
}

// FailWorkerJobは、ジョブの失敗を記録し、関連リソースをクリーンアップする。
func (h *Handlers) FailWorkerJob(ctx context.Context, req api.FailWorkerJobRequestObject) (api.FailWorkerJobResponseObject, error) {

	if err := h.repo.FailJob(ctx, req.Id); err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			slog.Warn("worker job fail not found", slog.String("job_id", req.Id.String()))
			return api.FailWorkerJob404JSONResponse{Message: "not found"}, nil
		}

		slog.Error("fail worker job", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.FailWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if h.notifier != nil {
		h.notifier.Notify(req.Id)
	}

	if err := h.jobFile.RemoveJobImageDir(req.Id); err != nil {
		slog.Error("fail cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	slog.Info("worker job failed", slog.String("job_id", req.Id.String()))
	return api.FailWorkerJob204Response{}, nil
}