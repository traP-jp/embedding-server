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
// 枚数・サイズ上限のリクエスト検証は OpenAPI 側。ここは読み取り時のメモリ保護。
const maxImageUploadBytes = 20 << 20 // 20 MiB

const maxTextChars = 8192

const maxTextUploadBytes = maxTextChars * utf8.UTFMax

var (
	ErrEmbeddingImageTooLarge        = errors.New("image too large")
	ErrEmbeddingUnsupportedImageType = errors.New("unsupported image type")
	ErrEmbeddingInvalidMultipart     = errors.New("invalid multipart")
	ErrEmbeddingCannotReadUpload     = errors.New("cannot read upload")
)

// EmbeddingInput は埋め込みリクエストの入力を表す。
type EmbeddingInput struct {
	Text       string
	Images     [][]byte
	WebhookURL string
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
// 枚数・サイズ・未知フィールドなどは OpenAPI validator に任せる。
// ここでは multipart の実体読み取りと、画像マジックバイト検査・text の trim/空判定を行う。
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
		return EmbeddingInput{}, ErrEmbeddingInvalidMultipart
	}

	input := EmbeddingInput{}
	for {
		part, err := req.Multipart.NextPart()
		if errors.Is(err, io.EOF) {
			// 正常終了
			break
		}
		if err != nil {
			return EmbeddingInput{}, ErrEmbeddingInvalidMultipart
		}

		var partErr error
		switch part.FormName() {
		case "text":
			if req.Mode == EmbeddingInputImages {
				// images endpoint では OpenAPI が弾く。直接呼び出し時のガード。
				partErr = ErrEmbeddingInvalidMultipart
				break
			}
			b, err := io.ReadAll(io.LimitReader(part, maxTextUploadBytes+1))
			if err != nil || len(b) > maxTextUploadBytes {
				partErr = ErrEmbeddingInvalidMultipart
				break
			}
			input.Text, partErr = normalizeEmbeddingText(string(b))
		case "images":
			raw, err := io.ReadAll(io.LimitReader(part, maxImageUploadBytes+1))
			if err != nil {
				partErr = ErrEmbeddingCannotReadUpload
				break
			}
			// +1まで読み込んでいるので、上限を超えているかどうかはlen(raw)で判断できる。
			if len(raw) > maxImageUploadBytes {
				partErr = ErrEmbeddingImageTooLarge
				break
			}

			switch http.DetectContentType(raw) {
			case "image/png", "image/jpeg", "image/webp":
				input.Images = append(input.Images, raw)
			default:
				partErr = ErrEmbeddingUnsupportedImageType
			}
		case "webhook_url":
			b, err := io.ReadAll(io.LimitReader(part, 2048+1))
			if err != nil || len(b) > 2048 {
				partErr = ErrEmbeddingInvalidMultipart
				break
			}
			input.WebhookURL = strings.TrimSpace(string(b))
		default:
			partErr = ErrEmbeddingInvalidMultipart
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
	return text, nil
}
