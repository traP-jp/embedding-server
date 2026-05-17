//go:generate mockgen -source=$GOFILE -destination=mock_$GOPACKAGE/mock_$GOFILE
package repository

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrCacheNotFound = errors.New("cache not found")

type CacheRepository interface {
	GetTextCache(ctx context.Context, text string) (json.RawMessage, error)
	SetTextCache(ctx context.Context, text string, value json.RawMessage) error
}
