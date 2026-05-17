package router

import (
	"embedding-server/api/repository"
	"embedding-server/api/service"
)

// Handlersは、HTTPハンドラメソッドが必要とする依存関係を保持する。
type Handlers struct {
	embedding *service.EmbeddingService
	repo      repository.Repository
	notifier  service.JobNotifier
	jobFile   *service.JobFileService
}

// NewHandlersは、指定された依存関係を持つ新しいHandlersインスタンスを生成する。
func NewHandlers(repo repository.Repository, notifier service.JobNotifier, embedding *service.EmbeddingService, jobFile *service.JobFileService) *Handlers {
	return &Handlers{
		embedding: embedding,
		repo:      repo,
		notifier:  notifier,
		jobFile:   jobFile,
	}
}
