package service

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// 画像をローカルに保存する関数。将来はオブジェクトストレージにしてもいいかも
func writeJobImage(jobID uuid.UUID, filename string, raw []byte) (string, error) {
	jobDir := jobImageDir(jobID)
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		return "", err
	}

	// 許可した画像拡張子だけ保存名に使う。
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		ext = ".bin"
	}

	path := filepath.Join(jobDir, "input"+ext)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func removeJobImageDir(jobID uuid.UUID) error {
	return os.RemoveAll(jobImageDir(jobID))
}

func jobImageDir(jobID uuid.UUID) string {
	return filepath.Join("/data/jobs", jobID.String())
}
