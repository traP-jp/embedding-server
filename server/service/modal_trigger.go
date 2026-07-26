package service

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type ModalTriggerConfig struct {
	Enable         bool
	URL            string
	Token          string
	BatchThreshold int
	MinInterval    time.Duration
	TriggerTimeout time.Duration
	ReclaimTTL     time.Duration
	ReclaimEvery   time.Duration
}

type modalTriggerRepo interface {
	CountPendingImageJobs(ctx context.Context) (int, error)
	CountProcessingImageJobs(ctx context.Context) (int, error)
	ReclaimStaleProcessingJobs(ctx context.Context, ttl time.Duration) (int64, error)
}

type ModalTrigger struct {
	cfg    ModalTriggerConfig
	client *http.Client
	repo   modalTriggerRepo

	mu          sync.Mutex
	lastTrigger time.Time
}

func NewModalTrigger(cfg ModalTriggerConfig, repo modalTriggerRepo) *ModalTrigger {
	return &ModalTrigger{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.TriggerTimeout},
		repo:   repo,
	}
}

func (t *ModalTrigger) Enabled() bool {
	return t != nil && t.cfg.Enable && t.cfg.URL != "" && t.cfg.Token != ""
}

// MaybeTrigger は pending 画像ジョブが閾値以上なら Modal を起動する（非同期）。
// processing 中の画像ジョブがあるときは二重起動を避ける。
// 起動条件は画像のみ。起動後の Modal は text も claim してついでに消化する。
func (t *ModalTrigger) MaybeTrigger(ctx context.Context) {
	if !t.Enabled() {
		return
	}
	processing, err := t.repo.CountProcessingImageJobs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "count processing image jobs for modal", slog.Any("error", err))
		// 不明時は起こしすぎるより抑える。
		return
	}
	if processing > 0 {
		slog.DebugContext(ctx, "modal trigger skipped (processing jobs exist)", slog.Int("processing", processing))
		return
	}
	count, err := t.repo.CountPendingImageJobs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "count pending image jobs for modal", slog.Any("error", err))
		return
	}
	if count < t.cfg.BatchThreshold {
		return
	}

	go func(ctx context.Context, pending int) {
		t.mu.Lock()
		if time.Since(t.lastTrigger) < t.cfg.MinInterval {
			t.mu.Unlock()
			slog.InfoContext(ctx, "modal trigger skipped (min interval)", slog.Int("pending", pending))
			return
		}
		t.lastTrigger = time.Now()
		t.mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.URL, nil)
		if err != nil {
			slog.ErrorContext(ctx, "modal trigger request build failed", slog.Any("error", err))
			return
		}
		// run_batch は query ?token= で認証（Modal fastapi_endpoint）
		q := req.URL.Query()
		q.Set("token", t.cfg.Token)
		req.URL.RawQuery = q.Encode()
		resp, err := t.client.Do(req)
		if err != nil {
			slog.WarnContext(ctx, "modal trigger failed", slog.Any("error", err))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			slog.WarnContext(ctx, "modal trigger non-2xx", slog.Int("status", resp.StatusCode), slog.Int("pending", pending))
			return
		}
		slog.InfoContext(ctx, "modal trigger ok", slog.Int("pending", pending), slog.Int("status", resp.StatusCode))
	}(context.WithoutCancel(ctx), count)
}

// RunReclaimLoop は古い processing を pending に戻し、残件があれば Modal を起こす。
func (t *ModalTrigger) RunReclaimLoop(ctx context.Context) {
	if t == nil || t.repo == nil {
		return
	}
	ticker := time.NewTicker(t.cfg.ReclaimEvery)
	defer ticker.Stop()

	slog.InfoContext(ctx, "modal reclaim loop started",
		slog.String("ttl", t.cfg.ReclaimTTL.String()),
		slog.String("every", t.cfg.ReclaimEvery.String()),
	)
	t.reclaimOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "modal reclaim loop stopped")
			return
		case <-ticker.C:
			t.reclaimOnce(ctx)
		}
	}
}

func (t *ModalTrigger) reclaimOnce(ctx context.Context) {
	count, err := t.repo.ReclaimStaleProcessingJobs(ctx, t.cfg.ReclaimTTL)
	if err != nil {
		slog.ErrorContext(ctx, "reclaim stale processing jobs", slog.Any("error", err))
		return
	}
	if count == 0 {
		return
	}
	slog.InfoContext(ctx, "reclaimed stale processing jobs", slog.Int64("count", count))
	t.MaybeTrigger(ctx)
}
