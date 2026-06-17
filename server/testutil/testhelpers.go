package testutil

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"embedding-server/api/repository"
	"embedding-server/api/repository/mock_repository"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

// MockNotifier is a test implementation of JobNotifier.
type MockNotifier struct {
	mu       sync.Mutex
	subs     map[uuid.UUID]chan struct{}
	unsubs   map[uuid.UUID]func()
	notifyCh chan uuid.UUID // records which job IDs were notified
}

func NewMockNotifier() *MockNotifier {
	return &MockNotifier{
		subs:     make(map[uuid.UUID]chan struct{}),
		unsubs:   make(map[uuid.UUID]func()),
		notifyCh: make(chan uuid.UUID, 100),
	}
}

func (n *MockNotifier) Subscribe(jobID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.subs[jobID] = ch
	n.mu.Unlock()

	unsub := func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.subs[jobID] == ch {
			delete(n.subs, jobID)
		}
	}
	n.mu.Lock()
	n.unsubs[jobID] = unsub
	n.mu.Unlock()
	return ch, unsub
}

func (n *MockNotifier) Notify(jobID uuid.UUID) {
	n.notifyCh <- jobID
	n.mu.Lock()
	ch, ok := n.subs[jobID]
	if ok {
		delete(n.subs, jobID)
	}
	n.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (n *MockNotifier) NotifiedIDs() []uuid.UUID {
	n.mu.Lock()
	defer n.mu.Unlock()
	var ids []uuid.UUID
	for {
		select {
		case id := <-n.notifyCh:
			ids = append(ids, id)
		default:
			return ids
		}
	}
}

// TestMocks holds gomock controller and both mock repositories.
type TestMocks struct {
	Ctrl  *gomock.Controller
	Job   *mock_repository.MockJobRepository
	Cache *mock_repository.MockCacheRepository
	Repo  repository.Repository
}

func NewTestMocks(t *testing.T) *TestMocks {
	t.Helper()
	ctrl := gomock.NewController(t)
	jobMock := mock_repository.NewMockJobRepository(ctrl)
	cacheMock := mock_repository.NewMockCacheRepository(ctrl)
	return &TestMocks{
		Ctrl:  ctrl,
		Job:   jobMock,
		Cache: cacheMock,
		Repo:  &combinedRepo{job: jobMock, cache: cacheMock},
	}
}

// combinedRepo implements repository.Repository by delegating to both mocks.
type combinedRepo struct {
	job   *mock_repository.MockJobRepository
	cache *mock_repository.MockCacheRepository
}

func (c *combinedRepo) GetJob(ctx context.Context, id uuid.UUID) (*repository.JobRecord, error) {
	return c.job.GetJob(ctx, id)
}
func (c *combinedRepo) CreateJob(ctx context.Context, input repository.CreateJobInput) error {
	return c.job.CreateJob(ctx, input)
}
func (c *combinedRepo) ClaimJob(ctx context.Context) (*repository.JobRecord, error) {
	return c.job.ClaimJob(ctx)
}
func (c *combinedRepo) GetJobState(ctx context.Context, id uuid.UUID) (repository.JobState, error) {
	return c.job.GetJobState(ctx, id)
}
func (c *combinedRepo) CompleteJob(ctx context.Context, id uuid.UUID, result json.RawMessage) error {
	return c.job.CompleteJob(ctx, id, result)
}
func (c *combinedRepo) FailJob(ctx context.Context, id uuid.UUID) error {
	return c.job.FailJob(ctx, id)
}
func (c *combinedRepo) CountPendingJobs(ctx context.Context) (int, error) {
	return c.job.CountPendingJobs(ctx)
}
func (c *combinedRepo) ExpiredJobImageKeys(ctx context.Context, ttl time.Duration) ([]string, error) {
	return c.job.ExpiredJobImageKeys(ctx, ttl)
}
func (c *combinedRepo) CleanupExpiredJobs(ctx context.Context, ttl time.Duration) (int64, error) {
	return c.job.CleanupExpiredJobs(ctx, ttl)
}
func (c *combinedRepo) GetTextCache(ctx context.Context, text string) (json.RawMessage, error) {
	return c.cache.GetTextCache(ctx, text)
}
func (c *combinedRepo) SetTextCache(ctx context.Context, text string, value json.RawMessage) error {
	return c.cache.SetTextCache(ctx, text, value)
}
func (c *combinedRepo) PruneCache(ctx context.Context) error {
	return c.cache.PruneCache(ctx)
}
