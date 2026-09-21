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
)

const webhookHTTPTimeout = 10 * time.Second

var ErrWebhookURLInvalid = errors.New("invalid webhook URL")

type WebhookDispatcher struct {
	client *http.Client
}

func NewWebhookDispatcher() *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{
			Transport: newWebhookTransport(),
			Timeout:   webhookHTTPTimeout,
			// 通知を別の URL に転送しない。
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (d *WebhookDispatcher) ValidateURL(webhookURL string) error {
	if webhookURL == "" {
		return nil
	}
	if d == nil {
		return ErrWebhookURLInvalid
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWebhookURLInvalid, err)
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || strings.TrimSpace(webhookURL) != webhookURL {
		return fmt.Errorf("%w: URL must use https with an absolute host", ErrWebhookURLInvalid)
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("%w: userinfo and fragments are not allowed", ErrWebhookURLInvalid)
	}
	if err := validateWebhookHost(u.Hostname()); err != nil {
		return err
	}
	return nil
}

func (d *WebhookDispatcher) Notify(ctx context.Context, webhookURL string, payload api.WebhookNotification) {
	if webhookURL == "" {
		return
	}
	if err := d.ValidateURL(webhookURL); err != nil {
		slog.WarnContext(ctx, "webhook URL rejected", slog.String("job_id", payload.Id.String()), slog.Any("error", err))
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "webhook marshal failed", slog.String("job_id", payload.Id.String()), slog.Any("error", err))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "webhook request build failed", slog.String("job_id", payload.Id.String()), slog.Any("error", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "webhook post failed", slog.String("job_id", payload.Id.String()), slog.Any("error", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.WarnContext(ctx, "webhook non-2xx", slog.String("job_id", payload.Id.String()), slog.Int("status", resp.StatusCode))
	}
}
