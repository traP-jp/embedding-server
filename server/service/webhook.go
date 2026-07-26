package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"embedding-server/api/api"
	"embedding-server/api/model"

	"github.com/google/uuid"
)

const webhookHTTPTimeout = 10 * time.Second

type WebhookPayload struct {
	ID     uuid.UUID            `json:"id"`
	Status model.JobStatus      `json:"status"`
	Result *api.EmbeddingResult `json:"result,omitempty"`
	Error  string               `json:"error,omitempty"`
}

type WebhookDispatcher struct {
	client *http.Client
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{Timeout: webhookHTTPTimeout},
	}
}

func (d *WebhookDispatcher) Notify(ctx context.Context, webhookURL string, payload WebhookPayload) {
	if webhookURL == "" {
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
