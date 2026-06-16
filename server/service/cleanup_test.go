package service

import (
	"context"
	"errors"
	"testing"

	"embedding-server/api/repository"
	"embedding-server/api/testutil"

	"go.uber.org/mock/gomock"
)

func TestCleanupService_cleanupExpiredJobs_DeletesExpired(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := newTestCleanupService(t, m.Repo)

	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return(nil, nil)
	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(3), nil)

	svc.cleanupExpiredJobs(context.Background())
	// パニックが発生しなければ成功。モックの期待値はgomockによって検証される
}

func TestCleanupService_cleanupExpiredJobs_KeepsWithinTTL(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := newTestCleanupService(t, m.Repo)

	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return(nil, nil)
	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(0), nil)

	svc.cleanupExpiredJobs(context.Background())
}

func TestCleanupService_cleanupExpiredJobs_Error(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := newTestCleanupService(t, m.Repo)

	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return(nil, nil)
	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(0), errors.New("db error"))

	// パニックしてはならない - エラーはログに記録されて握りつぶされる
	svc.cleanupExpiredJobs(context.Background())
}

func TestCleanupService_cleanupExpiredJobs_RemovesImageObjects(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile, fake := newFakeS3JobFileService(t)
	svc := NewCleanupService(m.Repo, jobFile)

	key := "jobs/test-job/0"
	fake.objects[key] = []byte("image")
	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return([]string{key}, nil)
	m.Job.EXPECT().CleanupExpiredJobs(gomock.Any(), jobTTL).Return(int64(1), nil)

	svc.cleanupExpiredJobs(context.Background())
	if fake.hasObject(key) {
		t.Fatalf("expected expired job image object %q to be removed", key)
	}
}

func TestCleanupService_cleanupExpiredJobs_ListImageKeysErrorSkipsRowDelete(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile := newTestJobFileService(t)
	svc := NewCleanupService(m.Repo, jobFile)

	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return(nil, errors.New("list error"))

	svc.cleanupExpiredJobs(context.Background())
}

func TestCleanupService_cleanupExpiredJobs_RemoveImageObjectsErrorSkipsRowDelete(t *testing.T) {
	m := testutil.NewTestMocks(t)
	jobFile, fake := newFakeS3JobFileService(t)
	svc := NewCleanupService(m.Repo, jobFile)

	key := "jobs/test-job/0"
	fake.objects[key] = []byte("image")
	fake.failDelete = true
	m.Job.EXPECT().ExpiredJobImageKeys(gomock.Any(), jobTTL).Return([]string{key}, nil)

	svc.cleanupExpiredJobs(context.Background())
	if !fake.hasObject(key) {
		t.Fatalf("expected image object %q to remain when delete fails", key)
	}
}

func TestCleanupService_pruneCache_Success(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := newTestCleanupService(t, m.Repo)

	m.Cache.EXPECT().PruneCache(gomock.Any()).Return(nil)

	svc.pruneCache(context.Background())
}

func TestCleanupService_pruneCache_Error(t *testing.T) {
	m := testutil.NewTestMocks(t)
	svc := newTestCleanupService(t, m.Repo)

	m.Cache.EXPECT().PruneCache(gomock.Any()).Return(errors.New("cache error"))

	// パニックしてはならない - エラーはログに記録されて握りつぶされる
	svc.pruneCache(context.Background())
}

func newTestCleanupService(t *testing.T, repo repository.Repository) *CleanupService {
	t.Helper()
	return NewCleanupService(repo, newTestJobFileService(t))
}
