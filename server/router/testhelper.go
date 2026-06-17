package router

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"
	"embedding-server/api/repository/mock_repository"
	"embedding-server/api/service"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"go.uber.org/mock/gomock"
)

// testSetupは、ルーターテストに必要な全てのコンポーネントを保持する。
type testSetup struct {
	ctrl     *gomock.Controller
	echo     *echo.Echo
	job      *mock_repository.MockJobRepository
	cache    *mock_repository.MockCacheRepository
	repo     repository.Repository
	notifier *service.LocalJobNotifier
	jobFile  *service.JobFileService
	fakeS3   *fakeS3Server
	handlers *Handlers
}

func setupTest(t *testing.T) *testSetup {
	t.Helper()
	ctrl := gomock.NewController(t)
	jobMock := mock_repository.NewMockJobRepository(ctrl)
	cacheMock := mock_repository.NewMockCacheRepository(ctrl)
	repo := &testCombinedRepo{job: jobMock, cache: cacheMock}

	e := echo.New()
	notifier := service.NewLocalJobNotifier()
	jobFile, fakeS3 := newFakeS3JobFileService(t)
	embeddingSvc := service.NewEmbeddingService(repo, notifier, jobFile)
	handlers := NewHandlers(repo, notifier, embeddingSvc, jobFile)

	api.RegisterHandlers(e, api.NewStrictHandler(handlers, nil))

	return &testSetup{
		ctrl:     ctrl,
		echo:     e,
		job:      jobMock,
		cache:    cacheMock,
		repo:     repo,
		notifier: notifier,
		jobFile:  jobFile,
		fakeS3:   fakeS3,
		handlers: handlers,
	}
}

type fakeS3Server struct {
	server  *httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3JobFileService(t *testing.T) (*service.JobFileService, *fakeS3Server) {
	t.Helper()

	fake := &fakeS3Server{objects: map[string][]byte{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)

	jobFile, err := service.NewS3JobFileService(context.Background(), service.S3JobFileConfig{
		Endpoint:        fake.server.URL,
		Bucket:          "test-bucket",
		Region:          "auto",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
		Prefix:          "jobs",
	})
	if err != nil {
		t.Fatalf("new job file service: %v", err)
	}
	return jobFile, fake
}

func (f *fakeS3Server) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPut:
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.objects[key] = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		var req struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		for _, obj := range req.Objects {
			delete(f.objects, obj.Key)
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`))
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

func (f *fakeS3Server) putObject(key string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = body
}

func (f *fakeS3Server) hasObject(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

// testCombinedRepoは、両方のモックに委譲することでrepository.Repositoryを実装する。
type testCombinedRepo struct {
	job   *mock_repository.MockJobRepository
	cache *mock_repository.MockCacheRepository
}

func (c *testCombinedRepo) GetJob(ctx context.Context, id uuid.UUID) (*repository.JobRecord, error) {
	return c.job.GetJob(ctx, id)
}
func (c *testCombinedRepo) CreateJob(ctx context.Context, input repository.CreateJobInput) error {
	return c.job.CreateJob(ctx, input)
}
func (c *testCombinedRepo) ClaimJob(ctx context.Context) (*repository.JobRecord, error) {
	return c.job.ClaimJob(ctx)
}
func (c *testCombinedRepo) GetJobState(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
	return c.job.GetJobState(ctx, id)
}
func (c *testCombinedRepo) CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error {
	return c.job.CompleteJob(ctx, id, result)
}
func (c *testCombinedRepo) FailJob(ctx context.Context, id uuid.UUID) error {
	return c.job.FailJob(ctx, id)
}
func (c *testCombinedRepo) CountPendingJobs(ctx context.Context) (int, error) {
	return c.job.CountPendingJobs(ctx)
}
func (c *testCombinedRepo) ExpiredJobImageKeys(ctx context.Context, ttl time.Duration) ([]string, error) {
	return c.job.ExpiredJobImageKeys(ctx, ttl)
}
func (c *testCombinedRepo) CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error) {
	return c.job.CleanupExpiredJobs(ctx, ttl)
}
func (c *testCombinedRepo) GetTextCache(ctx context.Context, text string) (json.RawMessage, error) {
	return c.cache.GetTextCache(ctx, text)
}
func (c *testCombinedRepo) SetTextCache(ctx context.Context, text string, value json.RawMessage) error {
	return c.cache.SetTextCache(ctx, text, value)
}
func (c *testCombinedRepo) PruneCache(ctx context.Context) error {
	return c.cache.PruneCache(ctx)
}

// CacheRecorderは、キャッシュモックのレコーダーへの便利なアクセスを提供する。
func (s *testSetup) CacheRecorder() *mock_repository.MockCacheRepositoryMockRecorder {
	return s.cache.EXPECT()
}

// doRequestは、テスト用Echoインスタンスに対してHTTPリクエストを実行する。
func (s *testSetup) doRequest(t *testing.T, method, path string, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.echo.ServeHTTP(rec, req)
	return rec
}

// assertStatusは、レスポンスのステータスコードをチェックする。
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status code: got %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

// assertJSONBodyは、JSONボディをパースしてmapとして返す。
func assertJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse JSON body: %v (raw: %s)", err, rec.Body.String())
	}
	return body
}

// assertErrorMessageは、レスポンスに期待されるエラーメッセージが含まれていることを確認する。
func assertErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	body := assertJSONBody(t, rec)
	msg, ok := body["message"].(string)
	if !ok {
		t.Fatalf("expected message field in response, got: %v", body)
	}
	if msg != want {
		t.Errorf("message: got %q, want %q", msg, want)
	}
}

// assertRetryAfterは、Retry-Afterヘッダーをチェックする。
func assertRetryAfter(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	got := rec.Header().Get("Retry-After")
	if got == "" {
		t.Fatal("expected Retry-After header")
	}
	var val int
	if _, err := fmt.Sscanf(got, "%d", &val); err != nil {
		t.Fatalf("invalid Retry-After value: %q", got)
	}
	if val != want {
		t.Errorf("Retry-After: got %d, want %d", val, want)
	}
}

// buildMultipartBodyは、テスト用のマルチパートフォームボディを作成する。
func buildMultipartBody(t *testing.T, fields map[string][]byte, files map[string]string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for name, data := range fields {
		fw, err := writer.CreateFormField(name)
		if err != nil {
			t.Fatalf("failed to create form field: %v", err)
		}
		fw.Write(data)
	}

	for name, data := range files {
		fw, err := writer.CreateFormFile(name, name)
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		fw.Write([]byte(data))
	}

	writer.Close()
	return &buf, writer.FormDataContentType()
}

// buildMultipartBodyWithBytesは、ファイル用の生バイトデータを含むマルチパートフォームボディを作成する。
func buildMultipartBodyWithBytes(t *testing.T, fields map[string][]byte, files map[string][]byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for name, data := range fields {
		fw, err := writer.CreateFormField(name)
		if err != nil {
			t.Fatalf("failed to create form field: %v", err)
		}
		fw.Write(data)
	}

	for name, data := range files {
		fw, err := writer.CreateFormFile(name, name)
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		fw.Write(data)
	}

	writer.Close()
	return &buf, writer.FormDataContentType()
}

// ptrStringは、指定された文字列へのポインタを返す。
func ptrString(s string) *string {
	return &s
}
