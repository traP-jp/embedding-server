package router

import (
	"embedding-server/api/repository"
	"embedding-server/api/service"
)

type Handlers struct {
	Embedding *service.EmbeddingService
	repo      repository.Repository
	notifier  service.JobNotifier
}

func GetHandlers(repo repository.Repository, notifier service.JobNotifier, embedding *service.EmbeddingService) *Handlers {
	return &Handlers{
		Embedding: embedding,
		repo:      repo,
		notifier:  notifier,
	}
}
