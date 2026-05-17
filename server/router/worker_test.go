package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"embedding-server/api/api"
	"embedding-server/api/repository"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestClaimWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	payload := api.WorkerJobPayload{Text: ptrString("hello")}
	payloadRaw, _ := json.Marshal(payload)

	s.job.EXPECT().ClaimJob(gomock.Any()).Return(jobID, payloadRaw, nil)

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

	s.job.EXPECT().ClaimJob(gomock.Any()).Return(uuid.Nil, nil, repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestClaimWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	s.job.EXPECT().ClaimJob(gomock.Any()).Return(uuid.Nil, nil, repository.ErrJobNotFound)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestClaimWorkerJob_InvalidPayload(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	invalidPayload := json.RawMessage(`not valid json`)

	s.job.EXPECT().ClaimJob(gomock.Any()).Return(jobID, invalidPayload, nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/claim", "application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestCompleteWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	payload := api.WorkerJobPayload{Text: ptrString("hello")}
	payloadRaw, _ := json.Marshal(payload)

	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(payloadRaw, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	s.cache.EXPECT().SetTextCache(gomock.Any(), "hello", gomock.Any()).Return(nil)

	body := `{"result":{"vector":[0.1, 0.2]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_EmptyVector(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	body := `{"result":{"vector":[]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorMessage(t, rec, "result vector required")
}

func TestCompleteWorkerJob_JobNotFound(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(nil, repository.ErrJobNotFound)

	body := `{"result":{"vector":[0.1]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusNotFound)
	assertErrorMessage(t, rec, "not found")
}

func TestCompleteWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(nil, repository.ErrNoJob)

	body := `{"result":{"vector":[0.1]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestCompleteWorkerJob_TextJobCache(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	payload := api.WorkerJobPayload{Text: ptrString("test text")}
	payloadRaw, _ := json.Marshal(payload)

	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(payloadRaw, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	s.cache.EXPECT().SetTextCache(gomock.Any(), "test text", gomock.Any()).Return(nil)

	body := `{"result":{"vector":[0.1, 0.2]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_ImageJobNoCache(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	imagePaths := []string{"/data/jobs/test/1.png"}
	payload := api.WorkerJobPayload{ImagePaths: &imagePaths}
	payloadRaw, _ := json.Marshal(payload)

	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(payloadRaw, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)
	// SetTextCacheの期待値は設定しない - 画像ジョブはキャッシュされないべき

	body := `{"result":{"vector":[0.1]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestCompleteWorkerJob_ImageDirCleanup(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	imagePaths, err := s.jobFile.WriteJobImages(jobID, [][]byte{pngHeader})
	if err != nil {
		t.Fatal(err)
	}
	payload := api.WorkerJobPayload{ImagePaths: &imagePaths}
	payloadRaw, _ := json.Marshal(payload)

	s.job.EXPECT().GetJobPayload(gomock.Any(), jobID).Return(payloadRaw, nil)
	s.job.EXPECT().CompleteJob(gomock.Any(), jobID, gomock.Any()).Return(nil)

	body := `{"result":{"vector":[0.1]}}`
	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/complete",
		"application/json", bytes.NewReader([]byte(body)))

	assertStatus(t, rec, http.StatusNoContent)
	if _, err := os.Stat(filepath.Dir(imagePaths[0])); !os.IsNotExist(err) {
		t.Fatalf("expected image directory to be removed, got err=%v", err)
	}
}

func TestFailWorkerJob_Success(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestFailWorkerJob_JobNotFound(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(repository.ErrJobNotFound)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNotFound)
	assertErrorMessage(t, rec, "not found")
}

func TestFailWorkerJob_InternalError(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(repository.ErrNoJob)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorMessage(t, rec, "internal error")
}

func TestFailWorkerJob_ImageDirCleanup(t *testing.T) {
	s := setupTest(t)

	jobID := uuid.New()
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	imagePaths, err := s.jobFile.WriteJobImages(jobID, [][]byte{pngHeader})
	if err != nil {
		t.Fatal(err)
	}
	s.job.EXPECT().FailJob(gomock.Any(), jobID).Return(nil)

	rec := s.doRequest(t, http.MethodPost, "/internal/worker/jobs/"+jobID.String()+"/fail",
		"application/json", nil)

	assertStatus(t, rec, http.StatusNoContent)
	if _, err := os.Stat(filepath.Dir(imagePaths[0])); !os.IsNotExist(err) {
		t.Fatalf("expected image directory to be removed, got err=%v", err)
	}
}
