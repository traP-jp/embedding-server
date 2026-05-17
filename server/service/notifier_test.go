package service

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLocalJobNotifier_SubscribeAndNotify(t *testing.T) {
	notifier := NewLocalJobNotifier()
	jobID := uuid.New()

	ch, unsub := notifier.Subscribe(jobID)

	// Notifyはチャネルをトリガーするべき
	notifier.Notify(jobID)

	select {
	case <-ch:
		// 成功
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}

	unsub()
}

func TestLocalJobNotifier_MultipleJobIDs(t *testing.T) {
	notifier := NewLocalJobNotifier()
	id1 := uuid.New()
	id2 := uuid.New()

	ch1, unsub1 := notifier.Subscribe(id1)
	ch2, unsub2 := notifier.Subscribe(id2)

	// id1のみ通知
	notifier.Notify(id1)

	select {
	case <-ch1:
		// 成功
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification on ch1")
	}

	// ch2は何も受信していないべき
	select {
	case <-ch2:
		t.Fatal("ch2 should not have received notification")
	default:
		// 成功
	}

	unsub1()
	unsub2()
}

func TestLocalJobNotifier_NotifyBeforeSubscribe(t *testing.T) {
	notifier := NewLocalJobNotifier()
	jobID := uuid.New()

	// 購読前に通知 - 何もしないはず
	notifier.Notify(jobID)

	// ここで購読
	ch, unsub := notifier.Subscribe(jobID)

	// チャネルは以前のnotifyによってトリガーされていないべき
	select {
	case <-ch:
		t.Fatal("channel should not have been triggered")
	default:
		// 成功
	}

	unsub()
}

func TestLocalJobNotifier_UnsubscribeStopsNotifications(t *testing.T) {
	notifier := NewLocalJobNotifier()
	jobID := uuid.New()

	ch, unsub := notifier.Subscribe(jobID)

	// 最初の通知は機能するべき
	notifier.Notify(jobID)
	select {
	case <-ch:
		// 成功
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first notification")
	}

	// 購読解除
	unsub()

	// 2回目の通知は（現在購読解除された）チャネルに届かないべき
	// チャネルはバッファされているため、空であることを確認する必要がある
	notifier.Notify(jobID)

	select {
	case <-ch:
		t.Fatal("channel should not have received notification after unsubscribe")
	default:
		// 成功
	}
}

func TestLocalJobNotifier_DuplicateNotification(t *testing.T) {
	notifier := NewLocalJobNotifier()
	jobID := uuid.New()

	ch, unsub := notifier.Subscribe(jobID)

	// 複数の通知を送信 - 1つのみ受信されるべき（バッファ付きチャネル）
	notifier.Notify(jobID)
	notifier.Notify(jobID)
	notifier.Notify(jobID)

	// 正確に1つ受信されるべき
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:

	if count != 1 {
		t.Errorf("expected 1 notification, got %d", count)
	}

	unsub()
}

func TestLocalJobNotifier_DifferentJobIDsNoCrossTalk(t *testing.T) {
	notifier := NewLocalJobNotifier()
	ids := make([]uuid.UUID, 5)
	channels := make([]<-chan struct{}, 5)
	unsubs := make([]func(), 5)

	for i := 0; i < 5; i++ {
		ids[i] = uuid.New()
		channels[i], unsubs[i] = notifier.Subscribe(ids[i])
	}

	// id 2のみ通知
	notifier.Notify(ids[2])

	// チャネル2のみ受信するべき
	for i, ch := range channels {
		select {
		case <-ch:
			if i != 2 {
				t.Errorf("channel %d received notification unexpectedly", i)
			}
		default:
			if i == 2 {
				t.Errorf("channel 2 did not receive notification")
			}
		}
	}

	for _, unsub := range unsubs {
		unsub()
	}
}

func TestLocalJobNotifier_ConcurrentSubscribeNotify(t *testing.T) {
	notifier := NewLocalJobNotifier()
	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			jobID := uuid.New()
			ch, unsub := notifier.Subscribe(jobID)
			notifier.Notify(jobID)
			select {
			case <-ch:
				// 成功
			case <-time.After(time.Second):
				t.Error("timed out waiting for notification")
			}
			unsub()
		}()
	}

	wg.Wait()
}
