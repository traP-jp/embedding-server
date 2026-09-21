package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"embedding-server/api/api"
	"embedding-server/api/repository"

	"github.com/google/uuid"
)

func TestWebhookDispatcherValidateURL(t *testing.T) {
	dispatcher := NewWebhookDispatcher()

	for name, tt := range map[string]struct {
		url     string
		wantErr bool
	}{
		"https URL":            {url: "https://hooks.example.com/callback?id=1"},
		"plain HTTP":           {url: "http://hooks.example.com/callback", wantErr: true},
		"userinfo is rejected": {url: "https://user@hooks.example.com/callback", wantErr: true},
		"fragment is rejected": {url: "https://hooks.example.com/callback#fragment", wantErr: true},
		"loopback":             {url: "https://127.0.0.1/callback", wantErr: true},
		"private":              {url: "https://10.0.0.1/callback", wantErr: true},
		"metadata":             {url: "https://169.254.169.254/", wantErr: true},
		"mapped IPv4":          {url: "https://[::ffff:127.0.0.1]/", wantErr: true},
		"localhost":            {url: "https://localhost./", wantErr: true},
		"no hostname":          {url: "https://:443/", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := dispatcher.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateURL(%q) error = %v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

type webhookRoundTripper func(*http.Request) (*http.Response, error)

func (f webhookRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEmbeddingServiceWebhookSendsOnlyJobIDAndStatus(t *testing.T) {
	for _, status := range []api.WebhookNotificationStatus{api.WebhookNotificationStatusCompleted, api.WebhookNotificationStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			job := &repository.JobRecord{ID: uuid.New(), WebhookURL: "https://hooks.example.com/callback"}
			dispatcher := NewWebhookDispatcher()
			calls := 0
			dispatcher.client.Transport = webhookRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.URL.String() != job.WebhookURL {
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
				}
				for _, header := range []string{"Authorization", "X-Webhook-Timestamp", "X-Webhook-Signature"} {
					if got := req.Header.Get(header); got != "" {
						t.Errorf("unexpected %s=%q", header, got)
					}
				}
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 2 || body["id"] != job.ID.String() || body["status"] != string(status) {
					t.Fatalf("expected only job id and status, got %v", body)
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})
			svc := NewEmbeddingService(nil, nil, nil, dispatcher, nil)
			if status == api.WebhookNotificationStatusCompleted {
				svc.NotifyWebhookCompleted(context.Background(), job)
			} else {
				svc.NotifyWebhookFailed(context.Background(), job)
			}
			if calls != 1 {
				t.Fatalf("requests=%d, want 1", calls)
			}
		})
	}
}

func TestWebhookDispatcherDoesNotFollowRedirects(t *testing.T) {
	dispatcher := NewWebhookDispatcher()
	calls := 0
	dispatcher.client.Transport = webhookRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	dispatcher.Notify(context.Background(), "https://hooks.example.com/callback", api.WebhookNotification{})
	if calls != 1 {
		t.Fatalf("requests=%d, want 1", calls)
	}
}

func TestEmbeddingServiceRejectsInvalidWebhookBeforeCreatingJob(t *testing.T) {
	dispatcher := NewWebhookDispatcher()
	svc := NewEmbeddingService(nil, nil, nil, dispatcher, nil)

	_, err := svc.CreateAsyncEmbedding(context.Background(), EmbeddingInput{
		Text:       "hello",
		WebhookURL: "http://example.com/callback",
	})
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid, got %v", err)
	}
}
