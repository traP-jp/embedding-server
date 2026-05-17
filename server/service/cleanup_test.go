package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"embedding-server/api/testutil"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestCleanupService_cleanupExpiredJobs_DeletesExpired(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(t.TempDir(), m.Repo)

	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(3), nil)

	svc.cleanupExpiredJobs(context.Background())
	// パニックが発生しなければ成功。モックの期待値はgomockによって検証される
}

func TestCleanupService_cleanupExpiredJobs_KeepsWithinTTL(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(t.TempDir(), m.Repo)

	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(0), nil)

	svc.cleanupExpiredJobs(context.Background())
}

func TestCleanupService_cleanupExpiredJobs_Error(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(t.TempDir(), m.Repo)

	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(0), errors.New("db error"))

	// パニックしてはならない - エラーはログに記録されて握りつぶされる
	svc.cleanupExpiredJobs(context.Background())
}

func TestCleanupService_cleanupImageDirs_ExpiredDirs(t *testing.T) {
	jobDir := t.TempDir()
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(jobDir, m.Repo)

	// 古い更新時刻のディレクトリを作成
	oldDir := filepath.Join(jobDir, uuid.New().String())
	newDir := filepath.Join(jobDir, uuid.New().String())
	os.MkdirAll(oldDir, 0o700)
	os.MkdirAll(newDir, 0o700)

	// oldDirの更新時刻を過去に設定
	oldTime := time.Now().Add(-7 * time.Hour)
	os.Chtimes(oldDir, oldTime, oldTime)

	svc.cleanupImageDirs(context.Background())

	// oldDirは削除されるべき
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("expected old directory to be removed")
	}

	// newDirは残っているべき
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("expected new directory to still exist")
	}
}

func TestCleanupService_cleanupImageDirs_KeepsWithinRetention(t *testing.T) {
	jobDir := t.TempDir()
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(jobDir, m.Repo)

	// 最近のディレクトリを作成
	recentDir := filepath.Join(jobDir, uuid.New().String())
	os.MkdirAll(recentDir, 0o700)

	svc.cleanupImageDirs(context.Background())

	// まだ存在しているべき
	if _, err := os.Stat(recentDir); os.IsNotExist(err) {
		t.Error("expected recent directory to still exist")
	}
}

func TestCleanupService_cleanupImageDirs_NonExistentDir(t *testing.T) {
	// 存在しないディレクトリを使用
	svc := NewCleanupService("/nonexistent/path/that/does/not/exist", nil)

	// パニックしてはならない - os.IsNotExistは処理される
	svc.cleanupImageDirs(context.Background())
}

func TestCleanupService_pruneCache_Success(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(t.TempDir(), m.Repo)

	m.Cache.EXPECT().PruneCache(gomock.Any()).Return(nil)

	svc.pruneCache(context.Background())
}

func TestCleanupService_pruneCache_Error(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(t.TempDir(), m.Repo)

	m.Cache.EXPECT().PruneCache(gomock.Any()).Return(errors.New("cache error"))

	// パニックしてはならない - エラーはログに記録されて握りつぶされる
	svc.pruneCache(context.Background())
}

func TestCleanupService_cleanupImageDirs_ContextCancellation(t *testing.T) {
	jobDir := t.TempDir()
	m := testutil.NewTestMocks(t)
	svc := NewCleanupService(jobDir, m.Repo)

	// 複数のディレクトリを作成
	dirs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		dir := filepath.Join(jobDir, uuid.New().String())
		os.MkdirAll(dir, 0o700)
		oldTime := time.Now().Add(-7 * time.Hour)
		os.Chtimes(dir, oldTime, oldTime)
		dirs = append(dirs, dir)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	svc.cleanupImageDirs(ctx)
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected canceled cleanup to leave %s untouched: %v", dir, err)
		}
	}
}
