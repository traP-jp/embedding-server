package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"embedding-server/api/model"
	"embedding-server/api/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(model.Models()...); err != nil {
		t.Fatal(err)
	}
	return &Repository{db: db}
}

// --- ジョブCRUDテスト ---

func TestRepository_CreateJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()
	payload := json.RawMessage(`{"text":"hello"}`)

	err := repo.CreateJob(ctx, jobID, payload)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJobState failed: %v", err)
	}
	if state.Status != repository.StatusPending {
		t.Errorf("expected status pending, got %s", state.Status)
	}
}

func TestRepository_GetJobPayload(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()
	payload := json.RawMessage(`{"text":"hello"}`)

	err := repo.CreateJob(ctx, jobID, payload)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetJobPayload(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJobPayload failed: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("got payload %s, want %s", got, payload)
	}
}

func TestRepository_GetJobPayload_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.GetJobPayload(ctx, uuid.New())
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

func TestRepository_GetJobState_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.GetJobState(ctx, uuid.New())
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

// --- Claim FIFOテスト ---

func TestRepository_ClaimJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()
	payload := json.RawMessage(`{"text":"hello"}`)

	err := repo.CreateJob(ctx, jobID, payload)
	if err != nil {
		t.Fatal(err)
	}

	id, gotPayload, err := repo.ClaimJob(ctx)
	if err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}
	if id != jobID {
		t.Errorf("got job ID %s, want %s", id, jobID)
	}
	if string(gotPayload) != string(payload) {
		t.Errorf("got payload %s, want %s", gotPayload, payload)
	}

	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusProcessing {
		t.Errorf("expected status processing, got %s", state.Status)
	}
}

func TestRepository_ClaimJob_NoJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, _, err := repo.ClaimJob(ctx)
	if !errors.Is(err, repository.ErrNoJob) {
		t.Errorf("got error %v, want ErrNoJob", err)
	}
}

func TestRepository_ClaimJob_FIFO(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	id1 := uuid.New()
	id2 := uuid.New()

	repo.CreateJob(ctx, id1, json.RawMessage(`{"text":"first"}`))
	time.Sleep(10 * time.Millisecond)
	repo.CreateJob(ctx, id2, json.RawMessage(`{"text":"second"}`))

	claimedID, _, err := repo.ClaimJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimedID != id1 {
		t.Errorf("expected first job %s, got %s", id1, claimedID)
	}
}

func TestRepository_ClaimJob_ConcurrentSafety(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	jobID := uuid.New()
	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"test"}`))

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successCount atomic.Int32
	errCh := make(chan error, workers)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			id, _, err := repo.ClaimJob(ctx)
			if err == nil {
				if id != jobID {
					errCh <- errors.New("claimed unexpected job id")
					return
				}
				successCount.Add(1)
				return
			}
			if !errors.Is(err, repository.ErrNoJob) {
				errCh <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected claim error: %v", err)
	}
	if successCount.Load() != 1 {
		t.Errorf("expected exactly 1 successful claim, got %d", successCount.Load())
	}
}

// --- 完了/失敗状態遷移テスト ---

func TestRepository_CompleteJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()

	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"hello"}`))
	repo.ClaimJob(ctx)

	result := json.RawMessage(`{"vector":[0.1, 0.2]}`)
	err := repo.CompleteJob(ctx, jobID, result)
	if err != nil {
		t.Fatalf("CompleteJob failed: %v", err)
	}

	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusCompleted {
		t.Errorf("expected status completed, got %s", state.Status)
	}
	if string(state.Result) != string(result) {
		t.Errorf("got result %s, want %s", state.Result, result)
	}
}

func TestRepository_CompleteJob_NotProcessing(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()

	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"hello"}`))
	// claimしない - ジョブはまだpending状態

	err := repo.CompleteJob(ctx, jobID, json.RawMessage(`{"vector":[0.1]}`))
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

func TestRepository_CompleteJob_EmptyResult(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()

	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"hello"}`))
	repo.ClaimJob(ctx)

	err := repo.CompleteJob(ctx, jobID, json.RawMessage(``))
	if err == nil {
		t.Error("expected error for empty result")
	}
}

func TestRepository_CompleteJob_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	err := repo.CompleteJob(ctx, uuid.New(), json.RawMessage(`{"vector":[0.1]}`))
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

func TestRepository_FailJob(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()

	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"hello"}`))
	repo.ClaimJob(ctx)

	err := repo.FailJob(ctx, jobID)
	if err != nil {
		t.Fatalf("FailJob failed: %v", err)
	}

	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusFailed {
		t.Errorf("expected status failed, got %s", state.Status)
	}
}

func TestRepository_FailJob_NotProcessing(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()
	jobID := uuid.New()

	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"hello"}`))

	err := repo.FailJob(ctx, jobID)
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

func TestRepository_FailJob_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	err := repo.FailJob(ctx, uuid.New())
	if !errors.Is(err, repository.ErrJobNotFound) {
		t.Errorf("got error %v, want ErrJobNotFound", err)
	}
}

// --- CountPendingテスト ---

func TestRepository_CountPendingJobs(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"a"}`))
	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"b"}`))
	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"c"}`))

	count, err := repo.CountPendingJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 pending jobs, got %d", count)
	}

	repo.ClaimJob(ctx)
	count, err = repo.CountPendingJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 pending jobs after claim, got %d", count)
	}
}

func TestRepository_CountPendingJobs_Zero(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	count, err := repo.CountPendingJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 pending jobs, got %d", count)
	}
}

// --- CleanupExpiredJobsテスト ---

func TestRepository_CleanupExpiredJobs(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"old1"}`))
	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"old2"}`))
	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"new"}`))

	// 期限切れをシミュレートするため、「古い」ジョブのcreated_atを手動で設定
	db := repo.db
	db.Model(&model.EmbeddingJob{}).
		Where("payload IN ?", []string{`{"text":"old1"}`, `{"text":"old2"}`}).
		Update("created_at", time.Now().Add(-7*time.Hour))

	deleted, err := repo.CleanupExpiredJobs(ctx, 6*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted jobs, got %d", deleted)
	}

	count, _ := repo.CountPendingJobs(ctx)
	if count != 1 {
		t.Errorf("expected 1 remaining job, got %d", count)
	}
}

func TestRepository_CleanupExpiredJobs_NoExpired(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	repo.CreateJob(ctx, uuid.New(), json.RawMessage(`{"text":"new"}`))

	deleted, err := repo.CleanupExpiredJobs(ctx, 6*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted jobs, got %d", deleted)
	}
}

// --- キャッシュCRUDテスト ---

func TestCache_GetTextCache_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.GetTextCache(ctx, "nonexistent")
	if !errors.Is(err, repository.ErrCacheNotFound) {
		t.Errorf("got error %v, want ErrCacheNotFound", err)
	}
}

func TestCache_SetAndGet(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	value := json.RawMessage(`{"vector":[0.1, 0.2]}`)
	err := repo.SetTextCache(ctx, "hello world", value)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetTextCache(ctx, "hello world")
	if err != nil {
		t.Fatalf("GetTextCache failed: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("got %s, want %s", got, value)
	}
}

func TestCache_SetTextCache_Overwrite(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	value1 := json.RawMessage(`{"vector":[0.1]}`)
	value2 := json.RawMessage(`{"vector":[0.2]}`)

	repo.SetTextCache(ctx, "hello", value1)
	repo.SetTextCache(ctx, "hello", value2)

	got, err := repo.GetTextCache(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(value2) {
		t.Errorf("got %s, want %s (overwritten value)", got, value2)
	}
}

// --- キャッシュキー正規化テスト ---

func TestCache_KeyNormalization(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	value := json.RawMessage(`{"vector":[0.1]}`)
	repo.SetTextCache(ctx, "  hello  ", value)

	// トリムされたテキストで見つかるべき
	got, err := repo.GetTextCache(ctx, "hello")
	if err != nil {
		t.Fatalf("GetTextCache failed: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("got %s, want %s", got, value)
	}
}

func TestCache_KeyNormalization_DifferentWhitespace(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	value := json.RawMessage(`{"vector":[0.1]}`)
	repo.SetTextCache(ctx, "\thello\n", value)

	got, err := repo.GetTextCache(ctx, "hello")
	if err != nil {
		t.Fatalf("GetTextCache failed: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("got %s, want %s", got, value)
	}
}

// --- PruneCacheテスト ---

func TestCache_PruneCache_UnderLimit(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		repo.SetTextCache(ctx, "text"+string(rune('a'+i)), json.RawMessage(`{"vector":[0.1]}`))
	}

	err := repo.PruneCache(ctx)
	if err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}

	// 全てのエントリがまだ存在しているべき（制限以下）
	_, err = repo.GetTextCache(ctx, "texta")
	if err != nil {
		t.Error("expected cache entry to still exist")
	}
}

func TestCache_PruneCache_OverLimit(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// maxEmbeddingCacheEntries（3000）を超える数を挿入
	// Use a smaller batch approach for SQLite performance
	const totalEntries = 3010
	const batchSize = 500

	for batch := 0; batch < (totalEntries / batchSize); batch++ {
		for i := 0; i < batchSize; i++ {
			key := "prune_test_" + string(rune('a'+batch)) + string(rune(i%26+'a')) + "_" + string(rune(i/26+'a'))
			repo.SetTextCache(ctx, key, json.RawMessage(`{"vector":[0.1]}`))
		}
	}
	// 残りのエントリを追加
	for i := 0; i < totalEntries%batchSize; i++ {
		key := "prune_remain_" + string(rune(i%26+'a'))
		repo.SetTextCache(ctx, key, json.RawMessage(`{"vector":[0.1]}`))
	}

	err := repo.PruneCache(ctx)
	if err != nil {
		t.Fatalf("PruneCache failed: %v", err)
	}

	// 一部のエントリがプルーニングされたことを確認
	var count int64
	repo.db.Model(&model.EmbeddingCache{}).Count(&count)
	if count >= int64(totalEntries) {
		t.Errorf("expected some entries to be pruned, got %d", count)
	}
}

// --- 完全ライフサイクルテスト ---

func TestRepository_FullLifecycle(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// 1. ジョブを作成
	jobID := uuid.New()
	payload := json.RawMessage(`{"text":"lifecycle test"}`)
	if err := repo.CreateJob(ctx, jobID, payload); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// 2. pending状態を確認
	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusPending {
		t.Errorf("expected pending, got %s", state.Status)
	}

	// 3. ジョブを取得
	claimedID, gotPayload, err := repo.ClaimJob(ctx)
	if err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}
	if claimedID != jobID {
		t.Errorf("claimed ID: got %s, want %s", claimedID, jobID)
	}
	if string(gotPayload) != string(payload) {
		t.Errorf("payload mismatch")
	}

	// 4. processing状態を確認
	state, err = repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusProcessing {
		t.Errorf("expected processing, got %s", state.Status)
	}

	// 5. ジョブを完了
	result := json.RawMessage(`{"vector":[1.0, 2.0, 3.0]}`)
	if err := repo.CompleteJob(ctx, jobID, result); err != nil {
		t.Fatalf("CompleteJob failed: %v", err)
	}

	// 6. 完了状態と結果を確認
	state, err = repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusCompleted {
		t.Errorf("expected completed, got %s", state.Status)
	}
	if string(state.Result) != string(result) {
		t.Errorf("result mismatch: got %s, want %s", state.Result, result)
	}

	// 7. ペイロードが引き続きアクセス可能であることを確認
	gotPayload2, err := repo.GetJobPayload(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJobPayload failed: %v", err)
	}
	if string(gotPayload2) != string(payload) {
		t.Errorf("payload mismatch after completion")
	}
}

func TestRepository_FailLifecycle(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	jobID := uuid.New()
	repo.CreateJob(ctx, jobID, json.RawMessage(`{"text":"fail test"}`))
	repo.ClaimJob(ctx)

	if err := repo.FailJob(ctx, jobID); err != nil {
		t.Fatalf("FailJob failed: %v", err)
	}

	state, err := repo.GetJobState(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != repository.StatusFailed {
		t.Errorf("expected failed, got %s", state.Status)
	}
}
