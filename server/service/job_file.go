package service

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
)

const defaultJobDataDir = "/data/jobs"

var errUnsupportedJobImageType = errors.New("unsupported job image type")

type JobFileService struct {
	dataDir string
}

func NewJobFileService(dataDir string) *JobFileService {
	if dataDir == "" {
		dataDir = defaultJobDataDir
	}
	return &JobFileService{dataDir: dataDir}
}

func (s *JobFileService) WriteJobImages(jobID uuid.UUID, images [][]byte) ([]string, error) {
	jobDir := s.jobImageDir(jobID)
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

func (s *JobFileService) RemoveJobImageDir(jobID uuid.UUID) error {
	return os.RemoveAll(s.jobImageDir(jobID))
}

func (s *JobFileService) DataDir() string {
	return s.dataDir
}

func (s *JobFileService) jobImageDir(jobID uuid.UUID) string {
	return filepath.Join(s.dataDir, jobID.String())
}
