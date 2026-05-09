package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"
)

const maxImageUploadBytes = 25 << 20 // 25 MiB

const syncEmbeddingWaitTimeout = 30 * time.Second

type waitResult struct {
	result api.EmbeddingResult
	status int
	err    error
}

func (h *Handlers) waitEmbeddingResult(ctx context.Context, id int64) waitResult {
	deadline := time.NewTimer(syncEmbeddingWaitTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	for {
		raw, ok, err := h.Repo.EmbeddingJobResult(ctx, id)
		if err != nil && !errors.Is(err, repository.ErrEmbeddingJobNotFound) {
			log.Printf("wait embedding result: %v", err)
			return waitResult{status: http.StatusInternalServerError, err: err}
		}
		if ok {
			result, err := parseEmbeddingResult(raw)
			if err != nil {
				log.Printf("parse embedding result: %v", err)
				return waitResult{status: http.StatusInternalServerError, err: err}
			}
			return waitResult{result: result, status: http.StatusOK}
		}

		select {
		case <-ctx.Done():
			return waitResult{status: http.StatusInternalServerError, err: ctx.Err()}
		case <-deadline.C:
			return waitResult{status: http.StatusGatewayTimeout}
		case <-tick.C:
		}
	}
}

func parseEmbeddingResult(raw json.RawMessage) (api.EmbeddingResult, error) {
	var result api.EmbeddingResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return api.EmbeddingResult{}, err
	}
	return result, nil
}

func textEmbeddingWaitResponse(w waitResult) api.PostEmbeddingsTextResponseObject {
	switch w.status {
	case http.StatusOK:
		return api.PostEmbeddingsText200JSONResponse(w.result)
	case http.StatusGatewayTimeout:
		return api.PostEmbeddingsText504JSONResponse{Message: "job processing timed out"}
	default:
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}
	}
}

func imageEmbeddingWaitResponse(w waitResult) api.PostEmbeddingsImageResponseObject {
	switch w.status {
	case http.StatusOK:
		return api.PostEmbeddingsImage200JSONResponse(w.result)
	case http.StatusGatewayTimeout:
		return api.PostEmbeddingsImage504JSONResponse{Message: "job processing timed out"}
	default:
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}
	}
}

func textImageEmbeddingWaitResponse(w waitResult) api.PostEmbeddingsTextImageResponseObject {
	switch w.status {
	case http.StatusOK:
		return api.PostEmbeddingsTextImage200JSONResponse(w.result)
	case http.StatusGatewayTimeout:
		return api.PostEmbeddingsTextImage504JSONResponse{Message: "job processing timed out"}
	default:
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}
	}
}

// PostEmbeddingsText はテキスト埋め込み用ジョブを作成する。
// 内部キャッシュに同一テキストの結果があればジョブを張らず完了行のみ作成する。
func (h *Handlers) PostEmbeddingsText(ctx context.Context, req api.PostEmbeddingsTextRequestObject) (api.PostEmbeddingsTextResponseObject, error) {
	if req.Body == nil {
		return api.PostEmbeddingsText400JSONResponse{Message: "invalid json"}, nil
	}
	text := strings.TrimSpace(req.Body.Text)
	if text == "" {
		return api.PostEmbeddingsText400JSONResponse{Message: "text required"}, nil
	}

	key := repository.TextEmbeddingCacheKey(text)
	if raw, err := h.Repo.CacheGet(ctx, key); err == nil {
		result, err := parseEmbeddingResult(raw)
		if err == nil {
			return api.PostEmbeddingsText200JSONResponse(result), nil
		}
		log.Printf("cache parse text: %v", err)
	} else if !errors.Is(err, repository.ErrEmbeddingCacheNotFound) {
		log.Printf("cache get text: %v", err)
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}

	payload, err := json.Marshal(map[string]string{
		"kind": "text",
		"text": text,
	})
	if err != nil {
		log.Printf("marshal text job: %v", err)
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}
	id, err := h.Repo.CreatePendingJob(ctx, payload)
	if err != nil {
		log.Printf("create text job: %v", err)
		return api.PostEmbeddingsText500JSONResponse{Message: "internal error"}, nil
	}
	return textEmbeddingWaitResponse(h.waitEmbeddingResult(ctx, id)), nil
}

// PostEmbeddingsImage は画像（multipart の image フィールド）の埋め込みジョブを作成する。
func (h *Handlers) PostEmbeddingsImage(ctx context.Context, req api.PostEmbeddingsImageRequestObject) (api.PostEmbeddingsImageResponseObject, error) {
	filename, raw, err := readMultipartImage(req.Body)
	if err != nil {
		if errors.Is(err, errImageTooLarge) {
			return api.PostEmbeddingsImage413JSONResponse{Message: "image too large"}, nil
		}
		return api.PostEmbeddingsImage400JSONResponse{Message: err.Error()}, nil
	}

	seedPayload, err := json.Marshal(map[string]any{"kind": "image"})
	if err != nil {
		log.Printf("marshal image seed: %v", err)
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}, nil
	}
	id, err := h.Repo.CreateJobWithStatus(ctx, seedPayload, "uploading")
	if err != nil {
		log.Printf("create image job: %v", err)
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}, nil
	}
	imagePath, err := writeJobImage(id, filename, raw)
	if err != nil {
		log.Printf("write image job: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}, nil
	}
	finalPayload, err := json.Marshal(map[string]any{
		"kind":       "image",
		"image_path": imagePath,
	})
	if err != nil {
		log.Printf("marshal image job: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}, nil
	}
	if err := h.Repo.UpdatePayloadAndStatus(ctx, id, "uploading", "pending", finalPayload); err != nil {
		log.Printf("update image payload: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsImage500JSONResponse{Message: "internal error"}, nil
	}
	return imageEmbeddingWaitResponse(h.waitEmbeddingResult(ctx, id)), nil
}

// PostEmbeddingsTextImage はテキストと画像をまとめた埋め込みジョブを作成する。
func (h *Handlers) PostEmbeddingsTextImage(ctx context.Context, req api.PostEmbeddingsTextImageRequestObject) (api.PostEmbeddingsTextImageResponseObject, error) {
	text, filename, raw, err := readMultipartTextImage(req.Body)
	if err != nil {
		if errors.Is(err, errImageTooLarge) {
			return api.PostEmbeddingsTextImage413JSONResponse{Message: "image too large"}, nil
		}
		return api.PostEmbeddingsTextImage400JSONResponse{Message: err.Error()}, nil
	}

	seedPayload, err := json.Marshal(map[string]any{
		"kind": "text_image",
		"text": text,
	})
	if err != nil {
		log.Printf("marshal text_image seed: %v", err)
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}, nil
	}
	id, err := h.Repo.CreateJobWithStatus(ctx, seedPayload, "uploading")
	if err != nil {
		log.Printf("create text_image job: %v", err)
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}, nil
	}
	imagePath, err := writeJobImage(id, filename, raw)
	if err != nil {
		log.Printf("write text_image job: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}, nil
	}
	finalPayload, err := json.Marshal(map[string]any{
		"kind":       "text_image",
		"text":       text,
		"image_path": imagePath,
	})
	if err != nil {
		log.Printf("marshal text_image job: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}, nil
	}
	if err := h.Repo.UpdatePayloadAndStatus(ctx, id, "uploading", "pending", finalPayload); err != nil {
		log.Printf("update text_image payload: %v", err)
		_ = h.Repo.FailUpload(ctx, id)
		return api.PostEmbeddingsTextImage500JSONResponse{Message: "internal error"}, nil
	}
	return textImageEmbeddingWaitResponse(h.waitEmbeddingResult(ctx, id)), nil
}

var (
	errImageFileRequired = errors.New("image file required")
	errTextRequired      = errors.New("text required")
	errImageTooLarge     = errors.New("image too large")
)

func readMultipartImage(reader *multipart.Reader) (filename string, raw []byte, err error) {
	if reader == nil {
		return "", nil, errors.New("invalid multipart")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return "", nil, errImageFileRequired
		}
		if err != nil {
			return "", nil, errors.New("invalid multipart")
		}
		defer part.Close()
		if part.FormName() != "image" {
			continue
		}
		raw, err := readLimited(part, maxImageUploadBytes)
		if err != nil {
			return "", nil, err
		}
		return part.FileName(), raw, nil
	}
}

func readMultipartTextImage(reader *multipart.Reader) (text string, filename string, raw []byte, err error) {
	if reader == nil {
		return "", "", nil, errors.New("invalid multipart")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", nil, errors.New("invalid multipart")
		}
		func() {
			defer part.Close()
			switch part.FormName() {
			case "text":
				b, readErr := io.ReadAll(io.LimitReader(part, 8193))
				if readErr != nil || len(b) > 8192 {
					err = errTextRequired
					return
				}
				text = strings.TrimSpace(string(b))
			case "image":
				filename = part.FileName()
				raw, err = readLimited(part, maxImageUploadBytes)
			}
		}()
		if err != nil {
			return "", "", nil, err
		}
	}
	if text == "" {
		return "", "", nil, errTextRequired
	}
	if len(raw) == 0 {
		return "", "", nil, errImageFileRequired
	}
	return text, filename, raw, nil
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

func writeJobImage(jobID int64, filename string, raw []byte) (string, error) {
	jobDir := filepath.Join("/data/jobs", strconv.FormatInt(jobID, 10))
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}
	path := filepath.Join(jobDir, "input"+ext)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
