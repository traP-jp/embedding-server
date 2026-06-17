package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"embedding-server/api/repository"
)

const (
	jobTTL             = 6 * time.Hour
	cachePruneInterval = 30 * time.Minute
)

type CleanupService struct {
	repo    repository.Repository
	jobFile *JobFileService
}

func NewCleanupService(repo repository.Repository, jobFile *JobFileService) *CleanupService {
	return &CleanupService{repo: repo, jobFile: jobFile}
}

func (s *CleanupService) Run(ctx context.Context) {
	jobTicker := time.NewTicker(jobTTL)
	defer jobTicker.Stop()

	cacheTicker := time.NewTicker(cachePruneInterval)
	defer cacheTicker.Stop()

	slog.Info("cleanup service started",
		slog.String("job_ttl", jobTTL.String()),
		slog.String("cache_prune_interval", cachePruneInterval.String()),
	)

	s.cleanupExpiredJobs(ctx)
	s.pruneCache(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("cleanup service stopped")
			return
		case <-jobTicker.C:
			s.cleanupExpiredJobs(ctx)
		case <-cacheTicker.C:
			s.pruneCache(ctx)
		}
	}
}

func (s *CleanupService) cleanupExpiredJobs(ctx context.Context) {
	if err := s.cleanupExpiredJobImages(ctx); err != nil {
		return
	}

	deleted, err := s.repo.CleanupExpiredJobs(ctx, jobTTL)
	if err != nil {
		slog.Error("cleanup expired jobs", slog.Any("error", err))
		return
	}
	if deleted > 0 {
		slog.Info("cleanup expired jobs", slog.Int64("deleted", deleted))
	}
}

func (s *CleanupService) cleanupExpiredJobImages(ctx context.Context) error {
	keys, err := s.repo.ExpiredJobImageKeys(ctx, jobTTL)
	if err != nil {
		slog.Error("list expired job image keys", slog.Any("error", err))
		return err
	}
	if len(keys) == 0 {
		return nil
	}

	if err := s.jobFile.RemoveJobImages(ctx, keys); err != nil {
		slog.Error("cleanup expired job images", slog.Int("object_count", len(keys)), slog.Any("error", err))
		return fmt.Errorf("cleanup expired job images: %w", err)
	}
	slog.Info("cleanup expired job images", slog.Int("object_count", len(keys)))
	return nil
}

func (s *CleanupService) pruneCache(ctx context.Context) {
	if err := s.repo.PruneCache(ctx); err != nil {
		slog.Error("prune cache", slog.Any("error", err))
	}
}
