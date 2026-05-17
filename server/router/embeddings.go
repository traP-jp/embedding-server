package router

import (
	"context"
	"errors"
	"log/slog"

	"embedding-server/api/api"
	"embedding-server/api/service"
)

const retryAfterSeconds = 30

func (h *Handlers) PostEmbeddingsText(ctx context.Context, req api.PostEmbeddingsTextRequestObject) (api.PostEmbeddingsTextResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode: service.EmbeddingInputText,
		Text: req.Body.Text,
	})
	if errors.Is(err, service.ErrEmbeddingInputRequired) {
		return api.PostEmbeddingsText400JSONResponse{Message: "text required"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingTextTooLong) {
		return api.PostEmbeddingsText400JSONResponse{Message: "text exceeds 8192 character limit"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsText400JSONResponse{Message: "invalid request"}, nil
	}

	result, err := h.Embedding.CreateEmbedding(ctx, input)
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

func (h *Handlers) PostEmbeddingsImages(ctx context.Context, req api.PostEmbeddingsImagesRequestObject) (api.PostEmbeddingsImagesResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode:      service.EmbeddingInputImages,
		Multipart: req.Body,
	})
	if errors.Is(err, service.ErrEmbeddingImageTooLarge) {
		return api.PostEmbeddingsImages413JSONResponse{Message: "image too large"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingUnsupportedImageType) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "unsupported image type"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingTooManyImages) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "too many images"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingTextNotAllowed) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "text is not allowed"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingInputRequired) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "images required"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsImages400JSONResponse{Message: "invalid request"}, nil
	}

	result, err := h.Embedding.CreateEmbedding(ctx, input)
	switch {
	case err == nil:
		return api.PostEmbeddingsImages200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingJobsFull):
		return api.PostEmbeddingsImages503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsImages503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsImages504JSONResponse{Message: "job processing timed out"}, nil
	default:
		slog.ErrorContext(ctx, "create embedding", slog.Any("error", err))
		return api.PostEmbeddingsImages500JSONResponse{Message: "internal error"}, nil
	}
}

func (h *Handlers) PostEmbeddingsMultimodal(ctx context.Context, req api.PostEmbeddingsMultimodalRequestObject) (api.PostEmbeddingsMultimodalResponseObject, error) {
	input, err := service.ReadEmbeddingInput(service.EmbeddingInputRequest{
		Mode:      service.EmbeddingInputMultimodal,
		Multipart: req.Body,
	})
	if errors.Is(err, service.ErrEmbeddingImageTooLarge) {
		return api.PostEmbeddingsMultimodal413JSONResponse{Message: "image too large"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingUnsupportedImageType) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "unsupported image type"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingTooManyImages) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "too many images"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingTextTooLong) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "text exceeds 8192 character limit"}, nil
	}
	if errors.Is(err, service.ErrEmbeddingInputRequired) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "text or images required"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "invalid request"}, nil
	}

	result, err := h.Embedding.CreateEmbedding(ctx, input)
	switch {
	case err == nil:
		return api.PostEmbeddingsMultimodal200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingInputRequired):
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "text or images required"}, nil
	case errors.Is(err, service.ErrEmbeddingJobsFull):
		return api.PostEmbeddingsMultimodal503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsMultimodal503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsMultimodal504JSONResponse{Message: "job processing timed out"}, nil
	default:
		slog.ErrorContext(ctx, "create embedding", slog.Any("error", err))
		return api.PostEmbeddingsMultimodal500JSONResponse{Message: "internal error"}, nil
	}
}
