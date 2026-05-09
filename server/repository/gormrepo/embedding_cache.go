package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"embedding-server/api/model"
	"embedding-server/api/repository"
)

// CacheGet は期限切れでないエントリのみ返す。
func (r *Repository) CacheGet(ctx context.Context, key string) (json.RawMessage, error) {
	var c model.EmbeddingCache
	err := r.db.WithContext(ctx).
		Where("key = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)", key).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrEmbeddingCacheNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(c.Value), nil
}

// CacheSet は UPSERT。
func (r *Repository) CacheSet(ctx context.Context, key string, value json.RawMessage, expiresAt *time.Time) error {
	c := model.EmbeddingCache{
		Key:       key,
		Value:     datatypes.JSON(value),
		ExpiresAt: expiresAt,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "expires_at"}),
	}).Create(&c).Error
}

func (r *Repository) CacheDelete(ctx context.Context, key string) error {
	res := r.db.WithContext(ctx).Where("key = ?", key).Delete(&model.EmbeddingCache{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return repository.ErrEmbeddingCacheNotFound
	}
	return nil
}
