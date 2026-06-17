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
	job, err := h.repo.ClaimJob(ctx)
	if errors.Is(err, repository.ErrNoJob) {
		return api.ClaimWorkerJob204Response{}, nil
	}
	if err != nil {
		slog.Error("claim worker job", slog.Any("error", err))
		return api.ClaimWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	payload := api.WorkerJobPayload{}
	if strings.TrimSpace(job.Text) != "" {
		payload.Text = &job.Text
	}
	if len(job.ImageObjectKeys) > 0 {
		imageObjects := make(api.EmbeddingImageObjects, len(job.ImageObjectKeys))
		for i, key := range job.ImageObjectKeys {
			imageObjects[i] = api.EmbeddingImageObject{Key: key}
		}
		payload.ImageObjects = &imageObjects
	}

	return api.ClaimWorkerJob200JSONResponse{
		Id:      job.ID,
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

	job, err := h.repo.GetJob(ctx, req.Id)
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

	h.notifier.Notify(req.Id)

	// テキスト埋め込みジョブの結果はキャッシュする。
	if strings.TrimSpace(job.Text) != "" && len(job.ImageObjectKeys) == 0 {
		if err := h.repo.SetTextCache(ctx, job.Text, resultRaw); err != nil {
			slog.Warn("complete cache set failed, continuing anyway", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		} else {
			slog.Debug("worker job result cached", slog.String("job_id", req.Id.String()), slog.Int("text_chars", len(job.Text)))
		}
	}

	if err := h.jobFile.RemoveJobImages(ctx, job.ImageObjectKeys); err != nil {
		slog.Error("complete cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	return api.CompleteWorkerJob204Response{}, nil
}

// FailWorkerJobは、ジョブの失敗を記録し、関連リソースをクリーンアップする。
func (h *Handlers) FailWorkerJob(ctx context.Context, req api.FailWorkerJobRequestObject) (api.FailWorkerJobResponseObject, error) {
	job, err := h.repo.GetJob(ctx, req.Id)
	if errors.Is(err, repository.ErrJobNotFound) {
		slog.Warn("worker job fail not found", slog.String("job_id", req.Id.String()))
		return api.FailWorkerJob404JSONResponse{Message: "not found"}, nil
	}
	if err != nil {
		slog.Error("fail load job", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.FailWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	if err := h.repo.FailJob(ctx, req.Id); err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			slog.Warn("worker job fail not found", slog.String("job_id", req.Id.String()))
			return api.FailWorkerJob404JSONResponse{Message: "not found"}, nil
		}

		slog.Error("fail worker job", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.FailWorkerJob500JSONResponse{Message: "internal error"}, nil
	}

	h.notifier.Notify(req.Id)

	if err := h.jobFile.RemoveJobImages(ctx, job.ImageObjectKeys); err != nil {
		slog.Error("fail cleanup image dir", slog.String("job_id", req.Id.String()), slog.Any("error", err))
	}

	slog.Info("worker job failed", slog.String("job_id", req.Id.String()))
	return api.FailWorkerJob204Response{}, nil
}
