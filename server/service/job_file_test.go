package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestJobFileService_WriteJobImages_PNG(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	paths, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}

	// ファイルが存在することを確認
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("file not found: %v", err)
	}

	// ファイル名のパターンを確認
	expectedName := "input-0.png"
	if filepath.Base(paths[0]) != expectedName {
		t.Errorf("expected filename %q, got %q", expectedName, filepath.Base(paths[0]))
	}

	// パーミッションを確認
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0o600, got %o", info.Mode().Perm())
	}
}

func TestJobFileService_WriteJobImages_JPEG(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	images := [][]byte{jpegHeader}

	paths, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedName := "input-0.jpg"
	if filepath.Base(paths[0]) != expectedName {
		t.Errorf("expected filename %q, got %q", expectedName, filepath.Base(paths[0]))
	}
}

func TestJobFileService_WriteJobImages_WebP(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	webpHeader := []byte{0x52, 0x49, 0x46, 0x46, 0x10, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x20}
	images := [][]byte{webpHeader}

	paths, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedName := "input-0.webp"
	if filepath.Base(paths[0]) != expectedName {
		t.Errorf("expected filename %q, got %q", expectedName, filepath.Base(paths[0]))
	}
}

func TestJobFileService_WriteJobImages_Multiple(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	images := [][]byte{pngHeader, jpegHeader}

	paths, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}

	if filepath.Base(paths[0]) != "input-0.png" {
		t.Errorf("expected filename %q, got %q", "input-0.png", filepath.Base(paths[0]))
	}
	if filepath.Base(paths[1]) != "input-1.jpg" {
		t.Errorf("expected filename %q, got %q", "input-1.jpg", filepath.Base(paths[1]))
	}
}

func TestJobFileService_WriteJobImages_UnsupportedType(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	images := [][]byte{[]byte("not an image")}

	_, err := svc.WriteJobImages(jobID, images)
	if !errors.Is(err, errUnsupportedJobImageType) {
		t.Fatalf("expected errUnsupportedJobImageType, got %v", err)
	}
}

func TestJobFileService_WriteJobImages_DirectoryCreation(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader}

	_, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ディレクトリが正しいパーミッションで作成されたことを確認
	jobDir := svc.jobImageDir(jobID)
	info, err := os.Stat(jobDir)
	if err != nil {
		t.Fatalf("directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("expected directory permissions 0o700, got %o", info.Mode().Perm())
	}
}

func TestJobFileService_WriteJobImages_FileNamingPattern(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	images := [][]byte{pngHeader, pngHeader, pngHeader}

	paths, err := svc.WriteJobImages(jobID, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDir := svc.jobImageDir(jobID)
	for i, p := range paths {
		expected := filepath.Join(expectedDir, "input-"+string(rune('0'+i))+".png")
		// 適切なインデックスフォーマットを使用
		expected = filepath.Join(expectedDir, "input-"+itoa(i)+".png")
		if p != expected {
			t.Errorf("path[%d]: got %q, want %q", i, p, expected)
		}
	}
}

func TestJobFileService_RemoveJobImageDir_Exists(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	_, err := svc.WriteJobImages(jobID, [][]byte{pngHeader})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.RemoveJobImageDir(jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ディレクトリが削除されたことを確認
	if _, err := os.Stat(svc.jobImageDir(jobID)); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestJobFileService_RemoveJobImageDir_NotExists(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewJobFileService(dataDir)
	jobID := uuid.New()

	// 存在しないディレクトリを削除 - エラーにならないはず（os.RemoveAllは冪等）
	err := svc.RemoveJobImageDir(jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJobFileService_DataDir_Custom(t *testing.T) {
	customDir := "/custom/data/path"
	svc := NewJobFileService(customDir)

	if svc.DataDir() != customDir {
		t.Errorf("expected DataDir %q, got %q", customDir, svc.DataDir())
	}
}

func TestJobFileService_DataDir_Default(t *testing.T) {
	svc := NewJobFileService("")

	if svc.DataDir() != defaultJobDataDir {
		t.Errorf("expected default DataDir %q, got %q", defaultJobDataDir, svc.DataDir())
	}
}

// itoaは、整数を文字列表現に変換する。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
