//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrEmbeddingCacheNotFound = errors.New("embedding cache not found")

type EmbeddingCacheRepository interface {
	CacheGet(ctx context.Context, key string) (json.RawMessage, error)
	CacheSet(ctx context.Context, key string, value json.RawMessage, expiresAt *time.Time) error
	CacheDelete(ctx context.Context, key string) error
}

// TextEmbeddingCacheKey は同一テキストの埋め込み結果を識別する内部キャッシュキー（ASCII）。
func TextEmbeddingCacheKey(trimmedUTF8Text string) string {
	sum := sha256.Sum256([]byte(trimmedUTF8Text))
	return "v1:text:" + hex.EncodeToString(sum[:])
}
