package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"embedding-server/api/model"

	"github.com/google/uuid"
)

func TestWebhookDispatcherValidateURL(t *testing.T) {
	dispatcher := NewWebhookDispatcher("external-secret")

	for name, tt := range map[string]struct {
		url     string
		wantErr bool
	}{
		"https URL":            {url: "https://hooks.example.com/callback?id=1"},
		"plain HTTP":           {url: "http://hooks.example.com/callback", wantErr: true},
		"userinfo is rejected": {url: "https://user@hooks.example.com/callback", wantErr: true},
		"fragment is rejected": {url: "https://hooks.example.com/callback#fragment", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := dispatcher.ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateURL(%q) error = %v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestWebhookDispatcherSendsExternalAPIKey(t *testing.T) {
	requestCh := make(chan *http.Request, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCh <- r.Clone(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dispatcher := NewWebhookDispatcher("external-secret")
	dispatcher.client.Transport = server.Client().Transport

	dispatcher.Notify(context.Background(), server.URL+"/callback", WebhookPayload{
		ID:     uuid.New(),
		Status: model.StatusCompleted,
	})

	select {
	case req := <-requestCh:
		if got := req.Header.Get("Authorization"); got != "Bearer external-secret" {
			t.Fatalf("Authorization=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook request was not received")
	}
}

func TestEmbeddingServiceRejectsInvalidWebhookBeforeCreatingJob(t *testing.T) {
	dispatcher := NewWebhookDispatcher("external-secret")
	svc := NewEmbeddingService(nil, nil, nil, dispatcher, nil)

	_, err := svc.CreateAsyncEmbedding(context.Background(), EmbeddingInput{
		Text:       "hello",
		WebhookURL: "http://example.com/callback",
	})
	if !errors.Is(err, ErrWebhookURLInvalid) {
		t.Fatalf("expected ErrWebhookURLInvalid, got %v", err)
	}
}
