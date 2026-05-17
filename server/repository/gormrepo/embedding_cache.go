package gormrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"embedding-server/api/model"
	"embedding-server/api/repository"
)

const maxEmbeddingCacheEntries = 3000

func (r *Repository) GetTextCache(ctx context.Context, text string) (json.RawMessage, error) {
	key := textEmbeddingCacheKey(text)
	cache, err := gorm.G[model.EmbeddingCache](r.db).
		Where("key = ?", key).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrCacheNotFound
	}
	if err != nil {
		return nil, err
	}

	// キャッシュにアクセスされたので last_accessed_at を更新する（LRU 削除のため）。
	if _, err := gorm.G[model.EmbeddingCache](r.db).
		Where("key = ?", key).
		Update(ctx, "last_accessed_at", time.Now()); err != nil {
		return nil, err
	}
	return json.RawMessage(cache.Value), nil
}

func (r *Repository) SetTextCache(ctx context.Context, text string, value json.RawMessage) error {
	// keyが重複したら上書き
	if err := gorm.G[model.EmbeddingCache](r.db, clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		UpdateAll: true,
	}).Create(ctx, &model.EmbeddingCache{
		Key:   textEmbeddingCacheKey(text),
		Value: datatypes.JSON(value),
	}); err != nil {
		return err
	}
	return pruneEmbeddingCache(ctx, r.db, maxEmbeddingCacheEntries)
}

func textEmbeddingCacheKey(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return "v1:text:" + hex.EncodeToString(sum[:])
}

func pruneEmbeddingCache(ctx context.Context, tx *gorm.DB, maxEntries int) error {
	count, err := gorm.G[model.EmbeddingCache](tx).Count(ctx, "*")
	if err != nil {
		return err
	}
	excess := count - int64(maxEntries)
	if excess <= 0 {
		return nil
	}

	subquery := gorm.G[model.EmbeddingCache](tx).
		Order("last_accessed_at ASC").
		Limit(int(excess)).
		Select("key")

	_, err = gorm.G[model.EmbeddingCache](tx).
		Where("key IN (?)", subquery).
		Delete(ctx)
	return err
}
