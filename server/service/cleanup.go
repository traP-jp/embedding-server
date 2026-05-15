package service

import (
	"context"
	"log"
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
	repo   repository.EmbeddingJobRepository
}

func NewCleanupService(jobDir string, repo repository.EmbeddingJobRepository) *CleanupService {
	return &CleanupService{jobDir: jobDir, repo: repo}
}

func (s *CleanupService) Run(ctx context.Context) {
	jobTicker := time.NewTicker(jobTTL)
	defer jobTicker.Stop()

	imageTicker := time.NewTicker(imageRetentionPeriod)
	defer imageTicker.Stop()

	log.Printf("cleanup service started: imageRetention=%s jobTTL=%s", imageRetentionPeriod, jobTTL)

	s.cleanupExpiredJobs(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("cleanup service stopped")
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
		log.Printf("cleanup expired jobs: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("cleanup expired jobs: deleted=%d", deleted)
	}
}

func (s *CleanupService) cleanupImageDirs(ctx context.Context) {
	entries, err := os.ReadDir(s.jobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("cleanup read dir: %v", err)
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
			log.Printf("cleanup stat dir=%s: %v", entry.Name(), err)
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(s.jobDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				log.Printf("cleanup remove dir=%s: %v", entry.Name(), err)
				continue
			}

			cleaned++
		}
	}

	if cleaned > 0 {
		log.Printf("cleanup image dirs: cleaned=%d", cleaned)
	}
}
