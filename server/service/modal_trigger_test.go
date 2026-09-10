package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type modalTriggerRepoStub struct {
	pending    int
	processing int
}

func (r modalTriggerRepoStub) CountPendingImageJobs(context.Context) (int, error) {
	return r.pending, nil
}

func (r modalTriggerRepoStub) CountProcessingImageJobs(context.Context) (int, error) {
	return r.processing, nil
}

func (modalTriggerRepoStub) ReclaimStaleProcessingJobs(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func TestModalTriggerSendsBearerAPIKey(t *testing.T) {
	requestCh := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCh <- r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	trigger := NewModalTrigger(ModalTriggerConfig{
		Enable:         true,
		URL:            server.URL,
		APIKey:         "internal-secret",
		BatchThreshold: 1,
		TriggerTimeout: time.Second,
	}, modalTriggerRepoStub{pending: 1})
	trigger.MaybeTrigger(context.Background())

	select {
	case req := <-requestCh:
		if got := req.Header.Get("Authorization"); got != "Bearer internal-secret" {
			t.Fatalf("Authorization=%q", got)
		}
		if req.URL.RawQuery != "" {
			t.Fatalf("API key must not be sent in query: %q", req.URL.RawQuery)
		}
	case <-time.After(time.Second):
		t.Fatal("Modal trigger request was not received")
	}
}
