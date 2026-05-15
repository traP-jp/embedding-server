package service

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"embedding-server/api/repository"

	"github.com/google/uuid"
)

const imageRetentionPeriod = 24 * time.Hour

type CleanupService struct {
	jobDir string
	repo   repository.EmbeddingJobRepository
}

func NewCleanupService(jobDir string, repo repository.EmbeddingJobRepository) *CleanupService {
	return &CleanupService{jobDir: jobDir, repo: repo}
}

func (s *CleanupService) Run(ctx context.Context) {
	ticker := time.NewTicker(imageRetentionPeriod)
	defer ticker.Stop()

	log.Printf("cleanup service started: retention=%s", imageRetentionPeriod)

	s.cleanup(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("cleanup service stopped")
			return
		case <-ticker.C:
			s.cleanup(ctx)
		}
	}
}

func (s *CleanupService) cleanup(ctx context.Context) {
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
			id, err := uuid.Parse(entry.Name())
			if err != nil {
				log.Printf("cleanup invalid job dir name=%s: %v", entry.Name(), err)
				continue
			}

			path := filepath.Join(s.jobDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				log.Printf("cleanup remove dir=%s: %v", entry.Name(), err)
				continue
			}

			if err := s.repo.DeleteJob(ctx, id); err != nil {
				log.Printf("cleanup delete job id=%s: %v", id, err)
				continue
			}

			cleaned++
		}
	}

	if cleaned > 0 {
		log.Printf("cleanup completed: cleaned=%d", cleaned)
	}
}
