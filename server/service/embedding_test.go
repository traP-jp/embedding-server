package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/repository"
	"embedding-server/api/testutil"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestCreateEmbedding_EmptyInput(t *testing.T) {
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(nil, nil, jobFile)

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{})
	if !errors.Is(err, ErrEmbeddingInputRequired) {
		t.Fatalf("expected ErrEmbeddingInputRequired, got %v", err)
	}
}

func TestCreateEmbedding_CacheHit(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	expected := api.EmbeddingResult{Vector: []float32{0.1, 0.2}}
	raw, _ := json.Marshal(expected)
	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(raw, nil)

	result, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 2 || result.Vector[0] != 0.1 || result.Vector[1] != 0.2 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCreateEmbedding_CacheParseError(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(json.RawMessage(`invalid`), nil)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return nil
		},
	)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.3]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(50 * time.Millisecond)
		notifier.Notify(id)
	}()

	result, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 0.3 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCreateEmbedding_CacheErrorNonNotFound(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	cacheErr := errors.New("database connection lost")
	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, cacheErr)

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, cacheErr) {
		t.Errorf("expected cache error, got %v", err)
	}
}

func TestCreateEmbedding_JobsFull(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(30, nil)

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if !errors.Is(err, ErrEmbeddingJobsFull) {
		t.Fatalf("expected ErrEmbeddingJobsFull, got %v", err)
	}
}

func TestCreateEmbedding_CreateJobFailure_ImageCleanup(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	dataDir := t.TempDir()
	jobFile := NewJobFileService(dataDir)
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return errors.New("db error")
		},
	)

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Images: images})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	capturedID := <-idCh
	jobDir := jobFile.jobImageDir(capturedID)
	if _, err := os.Stat(jobDir); err == nil {
		t.Error("expected image directory to be cleaned up")
	}
}

func TestCreateEmbedding_ImageInput(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	dataDir := t.TempDir()
	jobFile := NewJobFileService(dataDir)
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return nil
		},
	)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.7]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(50 * time.Millisecond)
		notifier.Notify(id)
	}()

	result, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Images: images})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 0.7 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCreateEmbedding_TextAndImageInput(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	dataDir := t.TempDir()
	jobFile := NewJobFileService(dataDir)
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return nil
		},
	)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.8]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(50 * time.Millisecond)
		notifier.Notify(id)
	}()

	result, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{
		Text:   "hello",
		Images: images,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 0.8 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCreateEmbedding_JobFailed(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return nil
		},
	)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{Status: repository.StatusFailed}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(50 * time.Millisecond)
		notifier.Notify(id)
	}()

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if !errors.Is(err, repository.ErrJobFailed) {
		t.Fatalf("expected ErrJobFailed, got %v", err)
	}
}

func TestCreateEmbedding_Timeout(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).Return(
		repository.JobState{Status: repository.StatusPending}, nil,
	).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := svc.CreateEmbedding(ctx, EmbeddingInput{Text: "hello"})
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrEmbeddingTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestCreateEmbedding_ContextCanceled(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).Return(
		repository.JobState{Status: repository.StatusPending}, nil,
	).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := svc.CreateEmbedding(ctx, EmbeddingInput{Text: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCreateEmbedding_NotifierNil(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	m.Cache.EXPECT().GetTextCache(gomock.Any(), "hello").Return(nil, repository.ErrCacheNotFound)
	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	_, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{Text: "hello"})
	if !errors.Is(err, ErrNotifierRequired) {
		t.Fatalf("expected ErrNotifierRequired, got %v", err)
	}
}

func TestCreateEmbedding_ImageCacheSkip(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	dataDir := t.TempDir()
	jobFile := NewJobFileService(dataDir)
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	m.Job.EXPECT().CountPendingJobs(gomock.Any()).Return(0, nil)

	idCh := make(chan uuid.UUID, 1)
	m.Job.EXPECT().CreateJob(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID, payload json.RawMessage) error {
			idCh <- id
			return nil
		},
	)
	m.Job.EXPECT().GetJobState(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[0.9]}`),
			}, nil
		},
	).AnyTimes()

	go func() {
		id := <-idCh
		time.Sleep(50 * time.Millisecond)
		notifier.Notify(id)
	}()

	result, err := svc.CreateEmbedding(context.Background(), EmbeddingInput{
		Text:   "hello",
		Images: images,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 0.9 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestWaitEmbeddingResult_ImmediateCompletion(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{
			Status: repository.StatusCompleted,
			Result: json.RawMessage(`{"vector":[1.0]}`),
		}, nil,
	)

	result, err := svc.waitEmbeddingResult(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 1.0 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestWaitEmbeddingResult_NotificationWait(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	jobID := uuid.New()

	callCount := 0
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).DoAndReturn(
		func(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
			callCount++
			if callCount == 1 {
				return repository.JobState{Status: repository.StatusProcessing}, nil
			}
			return repository.JobState{
				Status: repository.StatusCompleted,
				Result: json.RawMessage(`{"vector":[2.0]}`),
			}, nil
		},
	).Times(2)

	go func() {
		time.Sleep(100 * time.Millisecond)
		notifier.Notify(jobID)
	}()

	result, err := svc.waitEmbeddingResult(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 1 || result.Vector[0] != 2.0 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestWaitEmbeddingResult_Timeout(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{Status: repository.StatusProcessing}, nil,
	).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := svc.waitEmbeddingResult(ctx, jobID)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrEmbeddingTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestWaitEmbeddingResult_ContextCanceled(t *testing.T) {
	m := testutil.NewTestMocks(t)
	notifier := testutil.NewMockNotifier()
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, notifier, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{Status: repository.StatusProcessing}, nil,
	).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := svc.waitEmbeddingResult(ctx, jobID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestReadEmbeddingResult_Completed(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{
			Status: repository.StatusCompleted,
			Result: json.RawMessage(`{"vector":[0.1, 0.2, 0.3]}`),
		}, nil,
	)

	result, err := svc.readEmbeddingResult(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Vector) != 3 {
		t.Errorf("expected 3 vector elements, got %d", len(result.Vector))
	}
}

func TestReadEmbeddingResult_Failed(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{Status: repository.StatusFailed}, nil,
	)

	_, err := svc.readEmbeddingResult(context.Background(), jobID)
	if !errors.Is(err, repository.ErrJobFailed) {
		t.Fatalf("expected ErrJobFailed, got %v", err)
	}
}

func TestReadEmbeddingResult_Pending(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{Status: repository.StatusPending}, nil,
	)

	_, err := svc.readEmbeddingResult(context.Background(), jobID)
	if !errors.Is(err, errEmbeddingResultNotReady) {
		t.Fatalf("expected errEmbeddingResultNotReady, got %v", err)
	}
}

func TestReadEmbeddingResult_Processing(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{Status: repository.StatusProcessing}, nil,
	)

	_, err := svc.readEmbeddingResult(context.Background(), jobID)
	if !errors.Is(err, errEmbeddingResultNotReady) {
		t.Fatalf("expected errEmbeddingResultNotReady, got %v", err)
	}
}

func TestReadEmbeddingResult_JobNotFound(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{}, repository.ErrJobNotFound,
	)

	_, err := svc.readEmbeddingResult(context.Background(), jobID)
	if !errors.Is(err, errEmbeddingResultNotReady) {
		t.Fatalf("expected errEmbeddingResultNotReady, got %v", err)
	}
}

func TestReadEmbeddingResult_ParseError(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := NewJobFileService(t.TempDir())
	svc := NewEmbeddingService(m.Repo, nil, jobFile)

	jobID := uuid.New()
	m.Job.EXPECT().GetJobState(gomock.Any(), jobID).Return(
		repository.JobState{
			Status: repository.StatusCompleted,
			Result: json.RawMessage(`invalid json`),
		}, nil,
	)

	_, err := svc.readEmbeddingResult(context.Background(), jobID)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}
