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

func TestModalTriggerDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target must not receive internal API key")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	trigger := NewModalTrigger(ModalTriggerConfig{TriggerTimeout: time.Second}, nil)
	req, err := http.NewRequest(http.MethodPost, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer internal-secret")
	resp, err := trigger.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d", resp.StatusCode)
	}
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
