package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"embedding-server/api/model"
	"embedding-server/api/repository"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

const embeddingVectorDim = 4096

func completeRequestBody(t *testing.T) []byte {
	t.Helper()
	vector := make([]float32, embeddingVectorDim)
	vector[0] = 0.1
	if embeddingVectorDim > 1 {
		vector[1] = 0.2
	}
	body, err := json.Marshal(map[string]any{
		"result": map[string]any{"vector": vector},
	})
	if err != nil {
		t.Fatalf("marshal complete body: %v", err)
	}
	return body
}

func TestClaimWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().ClaimJob(gomock.Any(), gomock.Any()).Return(&repository.JobRecord{ID: jobID, Text: "hello"}, nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)

	// レスポンスにジョブIDとペイロードが含まれていることを確認
	id, ok := respBody["id"].(string)
	if !ok {
		t.Fatal("expected id in response")
	}
	if id != jobID.String() {
		t.Errorf("id: got %q, want %q", id, jobID.String())
	}

	payloadMap, ok := respBody["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("expected payload in response")
	}
	text, ok := payloadMap["text"].(string)
	if !ok {
		t.Fatal("expected text in payload")
	}
	if text != "hello" {
		t.Errorf("text: got %q, want %q", text, "hello")
	}
}

func TestClaimWorkerJob_NoJob(t *testing.T) {
	s := setupTest(t)

	s.job.EXPECT().ClaimJob(gomock.Any(), gomock.Any()).Return(&repository.JobRecord{}, repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestClaimWorkerJob_TextOnlyFilter(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().
		ClaimJob(gomock.Any(), repository.ClaimJobFilter{Kinds: []model.JobKind{model.JobKindText}}).
		Return(&repository.JobRecord{ID: jobID, Text: "hello"}, nil)

	body := bytes.NewBufferString(`{"kinds":["text"]}`)
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", body)

	assertStatus(t, rec, http.StatusOK)
	respBody := assertJSONBody(t, rec)
	if respBody["id"] != jobID.String() {
		t.Errorf("id: got %v, want %s", respBody["id"], jobID.String())
	}
}

func TestClaimWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	s.job.EXPECT().ClaimJob(gomock.Any(), gomock.Any()).Return(&repository.JobRecord{}, repository.ErrJobNotFound)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestCompleteWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID, Text: "hello", Kind: model.JobKindText}, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	s.cache.EXPECT().SetTextCache(gomock.Any(), "hello", gomock.Any()).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_EmptyVector(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	body := `{"result":{"vector":[]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "minimum number of items is 4096") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}

func TestCompleteWorkerJob_JobNotFound(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{}, repository.ErrJobNotFound)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusNotFound)
	assertErrorMessage(t, rec, "not found")
}

func TestCompleteWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{}, repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestCompleteWorkerJob_TextJobCache(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID, Text: "test text", Kind: model.JobKindText}, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	s.cache.EXPECT().SetTextCache(gomock.Any(), "test text", gomock.Any()).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_ImageJobNoCache(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{
		ID:              jobID,
		Kind:            model.JobKindImage,
		ImageObjectKeys: []string{"jobs/" + jobID.String() + "/0"},
	}, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	// SetTextCacheの期待値は設定しない - 画像ジョブはキャッシュされないべき

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_ImageDirCleanup(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	key := "jobs/" + jobID.String() + "/0"
	s.fakeS3.putObject(key, []byte("image"))

	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID, ImageObjectKeys: []string{key}}, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader(completeRequestBody(t)))

	assertStatus(t, rec, http.StatusNoContent)
	if s.fakeS3.hasObject(key) {
		t.Fatalf("expected image object %q to be removed", key)
	}
}

func TestFailWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID}, nil)
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestFailWorkerJob_JobNotFound(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{}, repository.ErrJobNotFound)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNotFound)
	assertErrorMessage(t, rec, "not found")
}

func TestFailWorkerJob_LoadJobError(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{}, repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestFailWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID}, nil)
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestFailWorkerJob_ImageDirCleanup(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	key := "jobs/" + jobID.String() + "/0"
	s.fakeS3.putObject(key, []byte("image"))

	s.job.EXPECT().GetJob(gomock.Any(), jobID).Return(&repository.JobRecord{ID: jobID, ImageObjectKeys: []string{key}}, nil)
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
	if s.fakeS3.hasObject(key) {
		t.Fatalf("expected image object %q to be removed", key)
	}
}
