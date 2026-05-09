package gormrepo

import (
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func GetRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}
