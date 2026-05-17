package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"embedding-server/api/repository"
)

const (
	imageRetentionPeriod = 6 * time.Hour
	jobTTL               = 6 * time.Hour
)

type CleanupService struct {
	jobDir string
	repo   repository.JobRepository
}

func NewCleanupService(jobDir string, repo repository.JobRepository) *CleanupService {
	return &CleanupService{jobDir: jobDir, repo: repo}
}

func (s *CleanupService) Run(ctx context.Context) {
	jobTicker := time.NewTicker(jobTTL)
	defer jobTicker.Stop()

	imageTicker := time.NewTicker(imageRetentionPeriod)
	defer imageTicker.Stop()

	slog.Info("cleanup service started",
		slog.String("image_retention", imageRetentionPeriod.String()),
		slog.String("job_ttl", jobTTL.String()),
	)

	s.cleanupExpiredJobs(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("cleanup service stopped")
			return
		case <-jobTicker.C:
			s.cleanupExpiredJobs(ctx)
		case <-imageTicker.C:
			s.cleanupImageDirs(ctx)
		}
	}
}

func (s *CleanupService) cleanupExpiredJobs(ctx context.Context) {
	deleted, err := s.repo.CleanupExpiredJobs(ctx, jobTTL)
	if err != nil {
		slog.Error("cleanup expired jobs", slog.Any("error", err))
		return
	}
	if deleted > 0 {
		slog.Info("cleanup expired jobs", slog.Int64("deleted", deleted))
	}
}

func (s *CleanupService) cleanupImageDirs(ctx context.Context) {
	entries, err := os.ReadDir(s.jobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		slog.Error("cleanup read dir", slog.Any("error", err))
		return
	}

	cutoff := time.Now().Add(-imageRetentionPeriod)
	var cleaned int

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}

		info, err := entry.Info()
		if err != nil {
			slog.Error("cleanup stat", slog.String("dir", entry.Name()), slog.Any("error", err))
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(s.jobDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				slog.Error("cleanup remove", slog.String("dir", entry.Name()), slog.Any("error", err))
				continue
			}

			cleaned++
		}
	}

	if cleaned > 0 {
		slog.Info("cleanup image dirs", slog.Int("cleaned", cleaned))
	}
}
