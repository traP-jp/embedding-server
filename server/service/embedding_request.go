package service

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"
)

// traqの画像の上限が20MB程度なので、同程度の上限を設ける。
const maxImageUploadBytes = 20 << 20 // 20 MiB

const maxEmbeddingImages = 4

const maxTextChars = 8192

const maxTextUploadBytes = maxTextChars * utf8.UTFMax

var (
	ErrEmbeddingImageTooLarge        = errors.New("image too large")
	ErrEmbeddingUnsupportedImageType = errors.New("unsupported image type")
	ErrEmbeddingTooManyImages        = errors.New("too many images")
	ErrEmbeddingTextTooLong          = errors.New("text too long")
	ErrEmbeddingTextNotAllowed       = errors.New("text not allowed")
)

// EmbeddingInput は埋め込みリクエストの入力を表す。
type EmbeddingInput struct {
	Text   string
	Images [][]byte
}

// EmbeddingInputRequest は埋め込み入力の読み取り元を表す。
type EmbeddingInputRequest struct {
	Mode      EmbeddingInputMode
	Text      string
	Multipart *multipart.Reader
}

// EmbeddingInputMode は受け付ける埋め込み入力種別を表す。
type EmbeddingInputMode int

const (
	// EmbeddingInputText はテキストのみを受け付ける。
	EmbeddingInputText EmbeddingInputMode = iota
	// EmbeddingInputImages は画像のみを受け付ける。
	EmbeddingInputImages
	// EmbeddingInputMultimodal はテキストと画像を受け付ける。
	EmbeddingInputMultimodal
)

// ReadEmbeddingInput はリクエストから埋め込み入力を読み取り、正規化と検査を行う。
func ReadEmbeddingInput(req EmbeddingInputRequest) (EmbeddingInput, error) {
	// textのみならすぐに返す
	if req.Mode == EmbeddingInputText {
		text, err := normalizeEmbeddingText(req.Text)
		if err != nil {
			return EmbeddingInput{}, err
		}
		return EmbeddingInput{Text: text}, nil
	}

	if req.Multipart == nil {
		return EmbeddingInput{}, errors.New("invalid multipart")
	}

	input := EmbeddingInput{}
	for {
		part, err := req.Multipart.NextPart()
		if errors.Is(err, io.EOF) {
			// 正常終了
			break
		}
		if err != nil {
			return EmbeddingInput{}, errors.New("invalid multipart")
		}

		var partErr error
		switch part.FormName() {
		case "text":
			if req.Mode == EmbeddingInputImages {
				partErr = ErrEmbeddingTextNotAllowed
				break
			}
			b, err := io.ReadAll(io.LimitReader(part, maxTextUploadBytes+1))
			if err != nil || len(b) > maxTextUploadBytes {
				partErr = ErrEmbeddingTextTooLong
				break
			}
			input.Text, partErr = normalizeEmbeddingText(string(b))
		case "images":
			// 画像が多すぎた場合は弾く
			if len(input.Images) >= maxEmbeddingImages {
				partErr = ErrEmbeddingTooManyImages
				break
			}
			raw, err := io.ReadAll(io.LimitReader(part, maxImageUploadBytes+1))
			if err != nil {
				partErr = errors.New("cannot read upload")
				break
			}
			// +1まで読み込んでいるので、上限を超えているかどうかはlen(raw)で判断できる。
			if len(raw) > maxImageUploadBytes {
				partErr = ErrEmbeddingImageTooLarge
				break
			}

			switch http.DetectContentType(raw) {
			case "image/png", "image/jpeg", "image/webp":
			default:
				partErr = ErrEmbeddingUnsupportedImageType
				break
			}
			input.Images = append(input.Images, raw)
		default:
			partErr = errors.New("invalid multipart")
		}
		part.Close()
		if partErr != nil {
			return EmbeddingInput{}, partErr
		}
	}

	switch {
	case req.Mode == EmbeddingInputImages && len(input.Images) == 0:
		return EmbeddingInput{}, ErrEmbeddingInputRequired
	case input.Text == "" && len(input.Images) == 0:
		return EmbeddingInput{}, ErrEmbeddingInputRequired
	default:
		return input, nil
	}
}

func normalizeEmbeddingText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrEmbeddingInputRequired
	}
	if utf8.RuneCountInString(text) > maxTextChars {
		return "", ErrEmbeddingTextTooLong
	}
	return text, nil
}
