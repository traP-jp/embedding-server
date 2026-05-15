package service

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
)

var errUnsupportedJobImageType = errors.New("unsupported job image type")

// 画像をローカルに保存する関数。将来はオブジェクトストレージにしてもいいかも
func writeJobImages(jobID uuid.UUID, images [][]byte) ([]string, error) {
	jobDir := jobImageDir(jobID)
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(images))
	for i, raw := range images {
		var ext string
		switch http.DetectContentType(raw) {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		default:
			return nil, errUnsupportedJobImageType
		}

		path := filepath.Join(jobDir, "input-"+strconv.Itoa(i)+ext)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func removeJobImageDir(jobID uuid.UUID) error {
	return os.RemoveAll(jobImageDir(jobID))
}

func jobImageDir(jobID uuid.UUID) string {
	return filepath.Join("/data/jobs", jobID.String())
}
