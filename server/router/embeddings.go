package router

import (
	"context"
	"errors"
	"log/slog"

	"embedding-server/api/api"
	"embedding-server/api/repository"
	"embedding-server/api/service"
)

const retryAfterSeconds = 30

// PostEmbeddingsTextは、指定されたテキスト入力に対する埋め込みを生成する。
func (h *Handlers) PostEmbeddingsText(ctx context.Context, req api.PostEmbeddingsTextRequestObject) (api.PostEmbeddingsTextResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode: service.EmbeddingInputText,
		Text: req.Body.Text,
	})
	if errors.Is(err, service.ErrEmbeddingInputRequired) {
		return api.PostEmbeddingsText400JSONResponse{Message: "text required"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsText400JSONResponse{Message: "invalid request"}, nil
	}

	result, err := h.embedding.CreateEmbedding(ctx, input)
	switch {
	case err == nil:
		return api.PostEmbeddingsText200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingJobsFull):
		return api.PostEmbeddingsText503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsText503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsText504JSONResponse{Message: "job processing timed out"}, nil
	default:
		slog.ErrorContext(ctx, "create embedding", slog.Any("error", err))
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}
}

// PostEmbeddingsImagesは、指定された画像入力に対する埋め込みジョブを受け付ける。
func (h *Handlers) PostEmbeddingsImages(ctx context.Context, req api.PostEmbeddingsImagesRequestObject) (api.PostEmbeddingsImagesResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode:      service.EmbeddingInputImages,
		Multipart: req.Body,
	})
	if errors.Is(err, service.ErrEmbeddingUnsupportedImageType) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "unsupported image type"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsImages400JSONResponse{Message: "invalid request"}, nil
	}

	id, err := h.embedding.CreateAsyncEmbedding(ctx, input)
	switch {
	case err == nil:
		return api.PostEmbeddingsImages202JSONResponse{Id: id}, nil
	default:
		slog.ErrorContext(ctx, "create embedding job", slog.Any("error", err))
		return api.PostEmbeddingsImages500JSONResponse{Message: "internal error"}, nil
	}
}

// PostEmbeddingsMultimodalは、指定されたテキストおよび/または画像入力に対する埋め込みジョブを受け付ける。
func (h *Handlers) PostEmbeddingsMultimodal(ctx context.Context, req api.PostEmbeddingsMultimodalRequestObject) (api.PostEmbeddingsMultimodalResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode:      service.EmbeddingInputMultimodal,
		Multipart: req.Body,
	})
	if errors.Is(err, service.ErrEmbeddingUnsupportedImageType) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "unsupported image type"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingInputRequired) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "text or images required"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "invalid request"}, nil
	}

	id, err := h.embedding.CreateAsyncEmbedding(ctx, input)
	switch {
	case err == nil:
		return api.PostEmbeddingsMultimodal202JSONResponse{Id: id}, nil
	default:
		slog.ErrorContext(ctx, "create embedding job", slog.Any("error", err))
		return api.PostEmbeddingsMultimodal500JSONResponse{Message: "internal error"}, nil
	}
}

// GetEmbeddingsJobは、埋め込みジョブの状態と結果を返す。
func (h *Handlers) GetEmbeddingsJob(ctx context.Context, req api.GetEmbeddingsJobRequestObject) (api.GetEmbeddingsJobResponseObject, error) {
	status, err := h.embedding.GetJobStatus(ctx, req.Id)
	switch {
	case err == nil:
		return api.GetEmbeddingsJob200JSONResponse(status), nil
	case errors.Is(err, repository.ErrJobNotFound):
		return api.GetEmbeddingsJob404JSONResponse{Message: "not found"}, nil
	default:
		slog.ErrorContext(ctx, "get embedding job", slog.String("job_id", req.Id.String()), slog.Any("error", err))
		return api.GetEmbeddingsJob500JSONResponse{Message: "internal error"}, nil
	}
}
