package router

import "embedding-server/api/repository"

type Handlers struct {
	Repo repository.Repository
}

func GetHandlers(repo repository.Repository) *Handlers {
	return &Handlers{
		Repo: repo,
	}
}
