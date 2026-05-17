package service

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

func TestReadEmbeddingInput_TextMode(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    string
		wantErr error
	}{
		{
			name: "valid text",
			text: "hello world",
			want: "hello world",
		},
		{
			name:    "whitespace only",
			text: "   ",
			wantErr: ErrEmbeddingInputRequired,
		},
		{
			name:    "empty",
			text:    "",
			wantErr: ErrEmbeddingInputRequired,
		},
		{
			name: "trim whitespace",
			text: "  hello  ",
			want: "hello",
		},
		{
			name:    "exceeds max chars",
			text:    strings.Repeat("a", maxTextChars+1),
			wantErr: ErrEmbeddingTextTooLong,
		},
		{
			name: "boundary max chars",
			text: strings.Repeat("a", maxTextChars),
			want: strings.Repeat("a", maxTextChars),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := ReadEmbeddingInput(EmbeddingInputRequest{
				Mode: EmbeddingInputText,
				Text: tt.text,
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if input.Text != tt.want {
				t.Errorf("got text %q, want %q", input.Text, tt.want)
			}
			if len(input.Images) != 0 {
				t.Errorf("expected no images, got %d", len(input.Images))
			}
		})
	}
}

func TestReadEmbeddingInput_ImagesMode(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	webpHeader := []byte{0x52, 0x49, 0x46, 0x46, 0x10, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x20}

	tests := []struct {
		name       string
		parts      []multipartPart
		wantImages int
		wantErr    error
	}{
		{
			name: "valid PNG",
			parts: []multipartPart{
				{name: "images", filename: "test.png", data: pngHeader},
			},
			wantImages: 1,
		},
		{
			name: "valid JPEG",
			parts: []multipartPart{
				{name: "images", filename: "test.jpg", data: jpegHeader},
			},
			wantImages: 1,
		},
		{
			name: "valid WebP",
			parts: []multipartPart{
				{name: "images", filename: "test.webp", data: webpHeader},
			},
			wantImages: 1,
		},
		{
			name: "multiple images",
			parts: []multipartPart{
				{name: "images", filename: "1.png", data: pngHeader},
				{name: "images", filename: "2.png", data: pngHeader},
			},
			wantImages: 2,
		},
		{
			name:       "no images",
			parts:      []multipartPart{},
			wantErr:    ErrEmbeddingInputRequired,
		},
		{
			name: "too many images",
			parts: []multipartPart{
				{name: "images", filename: "1.png", data: pngHeader},
				{name: "images", filename: "2.png", data: pngHeader},
				{name: "images", filename: "3.png", data: pngHeader},
				{name: "images", filename: "4.png", data: pngHeader},
				{name: "images", filename: "5.png", data: pngHeader},
			},
			wantErr: ErrEmbeddingTooManyImages,
		},
		{
			name: "unsupported format",
			parts: []multipartPart{
				{name: "images", filename: "test.gif", data: []byte("GIF89a")},
			},
			wantErr: ErrEmbeddingUnsupportedImageType,
		},
		{
			name: "text not allowed in images mode",
			parts: []multipartPart{
				{name: "text", data: []byte("hello")},
			},
			wantErr: ErrEmbeddingTextNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := buildMultipartReader(t, tt.parts)
			input, err := ReadEmbeddingInput(EmbeddingInputRequest{
				Mode:      EmbeddingInputImages,
				Multipart: reader,
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(input.Images) != tt.wantImages {
				t.Errorf("got %d images, want %d", len(input.Images), tt.wantImages)
			}
		})
	}
}

func TestReadEmbeddingInput_ImagesMode_LargeImage(t *testing.T) {
	// maxImageUploadBytesを超えるPNGのようなペイロードを作成
	largeData := make([]byte, maxImageUploadBytes+1)
	largeData[0] = 0x89
	largeData[1] = 0x50
	largeData[2] = 0x4E
	largeData[3] = 0x47

	reader := buildMultipartReader(t, []multipartPart{
		{name: "images", filename: "large.png", data: largeData},
	})

	_, err := ReadEmbeddingInput(EmbeddingInputRequest{
		Mode:      EmbeddingInputImages,
		Multipart: reader,
	})
	if !errors.Is(err, ErrEmbeddingImageTooLarge) {
		t.Fatalf("expected ErrEmbeddingImageTooLarge, got %v", err)
	}
}

func TestReadEmbeddingInput_MultimodalMode(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	tests := []struct {
		name       string
		parts      []multipartPart
		wantText   string
		wantImages int
		wantErr    error
	}{
		{
			name: "text and images",
			parts: []multipartPart{
				{name: "text", data: []byte("hello")},
				{name: "images", filename: "test.png", data: pngHeader},
			},
			wantText:   "hello",
			wantImages: 1,
		},
		{
			name: "text only",
			parts: []multipartPart{
				{name: "text", data: []byte("hello")},
			},
			wantText:   "hello",
			wantImages: 0,
		},
		{
			name: "images only",
			parts: []multipartPart{
				{name: "images", filename: "test.png", data: pngHeader},
			},
			wantText:   "",
			wantImages: 1,
		},
		{
			name:    "both empty",
			parts:   []multipartPart{},
			wantErr: ErrEmbeddingInputRequired,
		},
		{
			name: "text exceeds limit",
			parts: []multipartPart{
				{name: "text", data: []byte(strings.Repeat("a", maxTextChars+1))},
			},
			wantErr: ErrEmbeddingTextTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := buildMultipartReader(t, tt.parts)
			input, err := ReadEmbeddingInput(EmbeddingInputRequest{
				Mode:      EmbeddingInputMultimodal,
				Multipart: reader,
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if input.Text != tt.wantText {
				t.Errorf("got text %q, want %q", input.Text, tt.wantText)
			}
			if len(input.Images) != tt.wantImages {
				t.Errorf("got %d images, want %d", len(input.Images), tt.wantImages)
			}
		})
	}
}

func TestReadEmbeddingInput_InvalidMultipart(t *testing.T) {
	_, err := ReadEmbeddingInput(EmbeddingInputRequest{
		Mode:      EmbeddingInputImages,
		Multipart: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil multipart")
	}
}

// multipartPartは、マルチパートフォームの単一パートを表す。
type multipartPart struct {
	name     string
	filename string
	data     []byte
}

// buildMultipartReaderは、与えられたパートからmultipart.Readerを作成する。
func buildMultipartReader(t *testing.T, parts []multipartPart) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for _, p := range parts {
		var w io.Writer
		var err error
		if p.filename != "" {
			w, err = writer.CreateFormFile(p.name, p.filename)
		} else {
			w, err = writer.CreateFormField(p.name)
		}
		if err != nil {
			t.Fatalf("failed to create form field: %v", err)
		}
		if _, err := w.Write(p.data); err != nil {
			t.Fatalf("failed to write form data: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	reader := multipart.NewReader(&buf, writer.Boundary())
	return reader
}
