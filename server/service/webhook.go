package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/model"

	"github.com/google/uuid"
)

const webhookHTTPTimeout = 10 * time.Second

var ErrWebhookURLInvalid = errors.New("invalid webhook URL")

type WebhookPayload struct {
	ID     uuid.UUID            `json:"id"`
	Status model.JobStatus      `json:"status"`
	Result *api.EmbeddingResult `json:"result,omitempty"`
	Error  string               `json:"error,omitempty"`
}

type WebhookDispatcher struct {
	client *http.Client
	apiKey string
}

func NewWebhookDispatcher(apiKey string) *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{
			Timeout: webhookHTTPTimeout,
			// Authorization を別の URL へ転送しないよう redirect は追わない。
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		apiKey: apiKey,
	}
}

func (d *WebhookDispatcher) ValidateURL(webhookURL string) error {
	if webhookURL == "" {
		return nil
	}
	if d == nil {
		return ErrWebhookURLInvalid
	}

	u, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWebhookURLInvalid, err)
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return fmt.Errorf("%w: URL must use https with an absolute host", ErrWebhookURLInvalid)
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("%w: userinfo and fragments are not allowed", ErrWebhookURLInvalid)
	}
	return nil
}

func (d *WebhookDispatcher) Notify(ctx context.Context, webhookURL string, payload WebhookPayload) {
	if webhookURL == "" {
		return
	}
	if err := d.ValidateURL(webhookURL); err != nil {
		slog.WarnContext(ctx, "webhook URL rejected", slog.String("job_id", payload.ID.String()), slog.Any("error", err))
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "webhook marshal failed", slog.String("job_id", payload.ID.String()), slog.Any("error", err))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "webhook request build failed", slog.String("job_id", payload.ID.String()), slog.Any("error", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "webhook post failed", slog.String("job_id", payload.ID.String()), slog.Any("error", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.WarnContext(ctx, "webhook non-2xx", slog.String("job_id", payload.ID.String()), slog.Int("status", resp.StatusCode))
	}
}
