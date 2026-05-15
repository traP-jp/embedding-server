package router

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"embedding-server/api/api"
	"embedding-server/api/service"
)

// traqの画像の上限が20MB程度なので、同程度の上限を設ける。
const maxImageUploadBytes = 20 << 20 // 20 MiB

// workerが30s timeoutで処理するため、30件以上pendingがあれば503を返す。
const maxPendingJobs = 30

const retryAfterSeconds = 30

var (
	errImageTooLarge        = errors.New("image too large")
	errUnsupportedImageType = errors.New("unsupported image type")
	errTooManyImages        = errors.New("too many images")
)

// PostEmbeddingsText はテキスト埋め込み用ジョブを作成する。
// 内部キャッシュに同一テキストの結果があればジョブを張らず完了行のみ作成する。
func (h *Handlers) PostEmbeddingsText(ctx context.Context, req api.PostEmbeddingsTextRequestObject) (api.PostEmbeddingsTextResponseObject, error) {
	count, err := h.repo.CountPendingJobs(ctx)
	if err != nil {
		slog.Error("count pending jobs", slog.Any("error", err))
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}
	if count >= maxPendingJobs {
		return api.PostEmbeddingsText503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsText503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	}

	result, err := h.Embedding.CreateEmbedding(ctx, req.Body.Text, nil)
	switch {
	case err == nil:
		return api.PostEmbeddingsText200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingInputRequired):
		return api.PostEmbeddingsText400JSONResponse{Message: "text required"}, nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsText504JSONResponse{Message: "job processing timed out"}, nil
	default:
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}
}

// PostEmbeddingsImages は画像群の埋め込みジョブを作成する。
func (h *Handlers) PostEmbeddingsImages(ctx context.Context, req api.PostEmbeddingsImagesRequestObject) (api.PostEmbeddingsImagesResponseObject, error) {
	count, err := h.repo.CountPendingJobs(ctx)
	if err != nil {
		slog.Error("count pending jobs", slog.Any("error", err))
		return api.PostEmbeddingsImages500JSONResponse{Message: "internal error"}, nil
	}
	if count >= maxPendingJobs {
		return api.PostEmbeddingsImages503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsImages503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	}

	images, err := readMultipartImages(req.Body)
	if errors.Is(err, errImageTooLarge) {
		return api.PostEmbeddingsImages413JSONResponse{Message: "image too large"}, nil
	}
	if errors.Is(err, errUnsupportedImageType) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "unsupported image type"}, nil
	}
	if errors.Is(err, errTooManyImages) {
		return api.PostEmbeddingsImages400JSONResponse{Message: "too many images"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsImages400JSONResponse{Message: "invalid request"}, nil
	}

	result, err := h.Embedding.CreateEmbedding(ctx, "", images)
	switch {
	case err == nil:
		return api.PostEmbeddingsImages200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsImages504JSONResponse{Message: "job processing timed out"}, nil
	default:
		return api.PostEmbeddingsImages500JSONResponse{Message: "internal error"}, nil
	}
}

// PostEmbeddingsMultimodal はテキスト・画像群の埋め込みジョブを作成する。
func (h *Handlers) PostEmbeddingsMultimodal(ctx context.Context, req api.PostEmbeddingsMultimodalRequestObject) (api.PostEmbeddingsMultimodalResponseObject, error) {
	count, err := h.repo.CountPendingJobs(ctx)
	if err != nil {
		slog.Error("count pending jobs", slog.Any("error", err))
		return api.PostEmbeddingsMultimodal500JSONResponse{Message: "internal error"}, nil
	}
	if count >= maxPendingJobs {
		return api.PostEmbeddingsMultimodal503JSONResponse{
			Body:    api.ErrorResponse{Message: "too many pending jobs"},
			Headers: api.PostEmbeddingsMultimodal503ResponseHeaders{RetryAfter: retryAfterSeconds},
		}, nil
	}

	text, images, err := readMultipartMultimodal(req.Body)
	if errors.Is(err, errImageTooLarge) {
		return api.PostEmbeddingsMultimodal413JSONResponse{Message: "image too large"}, nil
	}
	if errors.Is(err, errUnsupportedImageType) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "unsupported image type"}, nil
	}
	if errors.Is(err, errTooManyImages) {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "too many images"}, nil
	}
	if err != nil {
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "invalid request"}, nil
	}

	var result api.EmbeddingResult
	switch {
	case len(images) == 0:
		result, err = h.Embedding.CreateEmbedding(ctx, text, nil)
	case text == "":
		result, err = h.Embedding.CreateEmbedding(ctx, "", images)
	default:
		result, err = h.Embedding.CreateEmbedding(ctx, text, images)
	}
	switch {
	case err == nil:
		return api.PostEmbeddingsMultimodal200JSONResponse(result), nil
	case errors.Is(err, service.ErrEmbeddingInputRequired):
		return api.PostEmbeddingsMultimodal400JSONResponse{Message: "text or images required"}, nil
	case errors.Is(err, service.ErrEmbeddingTimeout):
		return api.PostEmbeddingsMultimodal504JSONResponse{Message: "job processing timed out"}, nil
	default:
		return api.PostEmbeddingsMultimodal500JSONResponse{Message: "internal error"}, nil
	}
}

func readMultipartImages(reader *multipart.Reader) ([][]byte, error) {
	_, images, err := readMultipartMultimodal(reader)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, errors.New("images required")
	}
	return images, nil
}

func readMultipartMultimodal(reader *multipart.Reader) (text string, images [][]byte, err error) {
	if reader == nil {
		return "", nil, errors.New("invalid multipart")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, errors.New("invalid multipart")
		}
		func() {
			defer part.Close()
			switch part.FormName() {
			case "text":
				b, readErr := io.ReadAll(io.LimitReader(part, 8193))
				if readErr != nil || len(b) > 8192 {
					err = service.ErrEmbeddingInputRequired
					return
				}
				text = strings.TrimSpace(string(b))
			case "images":
				if len(images) >= 4 {
					err = errTooManyImages
					return
				}
				raw, readErr := readLimited(part, maxImageUploadBytes)
				err = readErr
				if err == nil {
					err = validateImageType(raw)
				}
				if err == nil {
					images = append(images, raw)
				}
			}
		}()
		if err != nil {
			return "", nil, err
		}
	}
	if text == "" && len(images) == 0 {
		return "", nil, errors.New("text or images required")
	}
	return text, images, nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, errors.New("cannot read upload")
	}
	if int64(len(raw)) > limit {
		return nil, errImageTooLarge
	}
	return raw, nil
}

func validateImageType(raw []byte) error {
	switch http.DetectContentType(raw) {
	case "image/png", "image/jpeg", "image/webp":
		return nil
	default:
		return errUnsupportedImageType
	}
}
