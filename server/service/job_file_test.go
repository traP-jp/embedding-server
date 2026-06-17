package service

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type fakeS3Server struct {
	server     *httptest.Server
	mu         sync.Mutex
	objects    map[string][]byte
	failDelete bool
}

func newTestJobFileService(t *testing.T) *JobFileService {
	t.Helper()
	svc, _ := newFakeS3JobFileService(t)
	return svc
}

func newFakeS3JobFileService(t *testing.T) (*JobFileService, *fakeS3Server) {
	t.Helper()

	fake := &fakeS3Server{objects: map[string][]byte{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)

	svc, err := NewS3JobFileService(context.Background(), S3JobFileConfig{
		Endpoint:        fake.server.URL,
		Bucket:          "test-bucket",
		Region:          "auto",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
		Prefix:          "jobs",
	})
	if err != nil {
		t.Fatalf("new job file service: %v", err)
	}
	return svc, fake
}

func (f *fakeS3Server) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPut:
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.objects[key] = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.mu.Lock()
		failDelete := f.failDelete
		f.mu.Unlock()
		if failDelete {
			http.Error(w, "delete failed", http.StatusInternalServerError)
			return
		}

		var req struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		for _, obj := range req.Objects {
			delete(f.objects, obj.Key)
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`))
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

func (f *fakeS3Server) hasObject(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

func (f *fakeS3Server) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

func TestJobFileService_StoreJobImages_PNG(t *testing.T) {
	svc, fake := newFakeS3JobFileService(t)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	stored, err := svc.StoreJobImages(context.Background(), jobID, [][]byte{pngHeader})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 object key, got %d", len(stored))
	}

	expectedKey := "jobs/" + jobID.String() + "/0"
	if stored[0] != expectedKey {
		t.Fatalf("object key: got %q, want %q", stored[0], expectedKey)
	}
	if !fake.hasObject(expectedKey) {
		t.Fatalf("expected object %q to be uploaded", expectedKey)
	}
}

func TestJobFileService_StoreJobImages_Multiple(t *testing.T) {
	svc, fake := newFakeS3JobFileService(t)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	stored, err := svc.StoreJobImages(context.Background(), jobID, [][]byte{pngHeader, jpegHeader})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 object keys, got %d", len(stored))
	}
	if !fake.hasObject("jobs/" + jobID.String() + "/0") {
		t.Fatal("expected first object to be uploaded")
	}
	if !fake.hasObject("jobs/" + jobID.String() + "/1") {
		t.Fatal("expected second object to be uploaded")
	}
}

func TestJobFileService_StoreJobImages_UnsupportedType(t *testing.T) {
	svc, _ := newFakeS3JobFileService(t)
	jobID := uuid.New()

	_, err := svc.StoreJobImages(context.Background(), jobID, [][]byte{[]byte("not an image")})
	if !errors.Is(err, errUnsupportedJobImageType) {
		t.Fatalf("expected errUnsupportedJobImageType, got %v", err)
	}
}

func TestJobFileService_StoreJobImages_UnsupportedTypeAfterValidImage(t *testing.T) {
	svc, fake := newFakeS3JobFileService(t)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	_, err := svc.StoreJobImages(context.Background(), jobID, [][]byte{pngHeader, []byte("not an image")})
	if !errors.Is(err, errUnsupportedJobImageType) {
		t.Fatalf("expected errUnsupportedJobImageType, got %v", err)
	}
	if fake.objectCount() != 0 {
		t.Fatalf("expected no uploaded objects, got %d", fake.objectCount())
	}
}

func TestJobFileService_RemoveJobImages(t *testing.T) {
	svc, fake := newFakeS3JobFileService(t)
	jobID := uuid.New()
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	stored, err := svc.StoreJobImages(context.Background(), jobID, [][]byte{pngHeader})
	if err != nil {
		t.Fatal(err)
	}
	if fake.objectCount() != 1 {
		t.Fatalf("expected 1 uploaded object, got %d", fake.objectCount())
	}

	if err := svc.RemoveJobImages(context.Background(), stored); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.objectCount() != 0 {
		t.Fatalf("expected all objects to be deleted, got %d", fake.objectCount())
	}
}

func TestNewS3JobFileService_InvalidConfig(t *testing.T) {
	_, err := NewS3JobFileService(context.Background(), S3JobFileConfig{})
	if !errors.Is(err, errInvalidS3JobFileConfig) {
		t.Fatalf("expected errInvalidS3JobFileConfig, got %v", err)
	}
}
