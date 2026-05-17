package gormrepo

import (
	"embedding-server/api/repository"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

var _ repository.Repository = (*Repository)(nil)

func GetRepository(db *gorm.DB) repository.Repository {
	return &Repository{db: db}
}
