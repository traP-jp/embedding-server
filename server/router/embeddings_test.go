package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestPostEmbeddingsText_Success(t *testing.T) {
	s := setupTest(t)

	s.cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID, payload interface{}) error {
			idCh <- id
			return nil
		},
	)

	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.1, 0.2]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(100 * time.Millisecond)
		s.notifier.Notify(id)
	}()

	body := `{"text":"hello"}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)
	vector, ok := respBody["vector"].([]interface{})
	if !ok {
		t.Fatalf("expected vector in response")
	}
	if len(vector) != 2 {
		t.Errorf("expected 2 vector elements, got %d", len(vector))
	}
}

func TestPostEmbeddingsText_EmptyText(t *testing.T) {
	s := setupTest(t)

	body := `{"text":""}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "text required")
}

func TestPostEmbeddingsText_TextTooLong(t *testing.T) {
	s := setupTest(t)

	longText := strings.Repeat("a", 8193)
	body := `{"text":"` + longText + `"}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "text exceeds 8192 character limit")
}

func TestPostEmbeddingsText_JobsFull(t *testing.T) {
	s := setupTest(t)

	s.cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(30, nil)

	body := `{"text":"hello"}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusServiceUnavailable)
	assertRetryAfter(t, rec, 30)
	assertErrorMessage(t, rec, "too many pending jobs")
}

func TestPostEmbeddingsText_Timeout(t *testing.T) {
	t.Skip("timeout test requires 30s wait - tested at service level")
}

func TestPostEmbeddingsText_InternalError(t *testing.T) {
	s := setupTest(t)

	s.cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	dbErr := errors.New("database connection lost")
	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).Return(
		repository.JobState{}, dbErr,
	).AnyTimes()

	body := `{"text":"hello"}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestPostEmbeddingsText_CacheHit(t *testing.T) {
	s := setupTest(t)

	expected := api.EmbeddingResult{Vector: []float32{0.5, 0.6}}
	raw, _ := json.Marshal(expected)
	s.cache.EXPECT().GetTextCache(gomock.Any(), "cached").Return(raw, nil)

	body := `{"text":"cached"}`
	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/text", "application/json", strings.NewReader(body))

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)
	vector, ok := respBody["vector"].([]interface{})
	if !ok {
		t.Fatalf("expected vector in response")
	}
	if len(vector) != 2 {
		t.Errorf("expected 2 vector elements, got %d", len(vector))
	}
}

// Images endpoint tests

func TestPostEmbeddingsImages_Success(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID, payload interface{}) error {
			idCh <- id
			return nil
		},
	)

	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.3]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(100 * time.Millisecond)
		s.notifier.Notify(id)
	}()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)
	if _, ok := respBody["vector"]; !ok {
		t.Fatal("expected vector in response")
	}
}

func TestPostEmbeddingsImages_NoImages(t *testing.T) {
	s := setupTest(t)

	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{})

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "images required")
}

func TestPostEmbeddingsImages_UnsupportedType(t *testing.T) {
	s := setupTest(t)

	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": []byte("GIF89a"),
	})

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "unsupported image type")
}

func TestPostEmbeddingsImages_TooManyImages(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for i := 0; i < 5; i++ {
		fw, _ := writer.CreateFormFile("images", "img.png")
		fw.Write(pngHeader)
	}
	writer.Close()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", writer.FormDataContentType(), &buf)

	assertStatus(t, rec, http.StatusBadRequest)
	body := assertJSONBody(t, rec)
	msg := body["message"].(string)
	if msg != "too many images" {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestPostEmbeddingsImages_TextNotAllowed(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte("hello"),
	}, map[string][]byte{
		"images": pngHeader,
	})

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "text is not allowed")
}

func TestPostEmbeddingsImages_ImageTooLarge(t *testing.T) {
	s := setupTest(t)

	largeData := make([]byte, 20<<20+1)
	largeData[0] = 0x89
	largeData[1] = 0x50
	largeData[2] = 0x4E
	largeData[3] = 0x47

	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": largeData,
	})

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusRequestEntityTooLarge)
	assertErrorMessage(t, rec, "image too large")
}

func TestPostEmbeddingsImages_JobsFull(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(30, nil)

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusServiceUnavailable)
	assertRetryAfter(t, rec, 30)
}

func TestPostEmbeddingsImages_InternalError(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	dbErr := errors.New("database connection lost")
	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).Return(
		repository.JobState{}, dbErr,
	).AnyTimes()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/images", contentType, body)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

// Multimodal endpoint tests

func TestPostEmbeddingsMultimodal_Success(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte("hello"),
	}, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID, payload interface{}) error {
			idCh <- id
			return nil
		},
	)

	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.4]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(100 * time.Millisecond)
		s.notifier.Notify(id)
	}()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)
	if _, ok := respBody["vector"]; !ok {
		t.Fatal("expected vector in response")
	}
}

func TestPostEmbeddingsMultimodal_TextOnly(t *testing.T) {
	s := setupTest(t)

	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte("hello"),
	}, nil)

	s.cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID, payload interface{}) error {
			idCh <- id
			return nil
		},
	)

	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.5]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(100 * time.Millisecond)
		s.notifier.Notify(id)
	}()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusOK)
}

func TestPostEmbeddingsMultimodal_ImagesOnly(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, nil, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID, payload interface{}) error {
			idCh <- id
			return nil
		},
	)

	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx interface{}, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.6]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(100 * time.Millisecond)
		s.notifier.Notify(id)
	}()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusOK)
}

func TestPostEmbeddingsMultimodal_BothEmpty(t *testing.T) {
	s := setupTest(t)

	body, contentType := buildMultipartBodyWithBytes(t, nil, nil)

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "text or images required")
}

func TestPostEmbeddingsMultimodal_TextTooLong(t *testing.T) {
	s := setupTest(t)

	longText := strings.Repeat("a", 8193)
	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte(longText),
	}, nil)

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "text exceeds 8192 character limit")
}

func TestPostEmbeddingsMultimodal_JobsFull(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte("hello"),
	}, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(30, nil)

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusServiceUnavailable)
	assertRetryAfter(t, rec, 30)
}

func TestPostEmbeddingsMultimodal_InternalError(t *testing.T) {
	s := setupTest(t)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body, contentType := buildMultipartBodyWithBytes(t, map[string][]byte{
		"text": []byte("hello"),
	}, map[string][]byte{
		"images": pngHeader,
	})

	s.job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	s.job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	dbErr := errors.New("database connection lost")
	s.job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).Return(
		repository.JobState{}, dbErr,
	).AnyTimes()

	rec := s.doRequest(t, http.MethodPost, "/v1/embeddings/multimodal", contentType, body)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}
