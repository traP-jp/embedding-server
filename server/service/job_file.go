package service

import (
	"os"
	"path/filepath"
	"strconv"
)

// 画像をローカルに保存する関数。将来はオブジェクトストレージにしてもいいかも
func writeJobImage(jobID int64, filename string, raw []byte) (string, error) {
	jobDir := filepath.Join("/data/jobs", strconv.FormatInt(jobID, 10))
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		return "", err
	}

	// 拡張子を取り出す
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}

	path := filepath.Join(jobDir, "input"+ext)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
