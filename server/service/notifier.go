package service

import (
	"sync"
)

type JobNotifier interface {
	Subscribe(jobID int64) (<-chan struct{}, func())
	Notify(jobID int64)
}

type LocalJobNotifier struct {
	mu      sync.Mutex
	waiters map[int64]chan struct{}
}

func NewLocalJobNotifier() *LocalJobNotifier {
	return &LocalJobNotifier{
		waiters: make(map[int64]chan struct{}),
	}
}

func (n *LocalJobNotifier) Subscribe(jobID int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.waiters[jobID] = ch
	n.mu.Unlock()

	unsubscribe := func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.waiters[jobID] == ch {
			delete(n.waiters, jobID)
		}
	}
	return ch, unsubscribe
}

func (n *LocalJobNotifier) Notify(jobID int64) {
	n.mu.Lock()
	waiter := n.waiters[jobID]
	n.mu.Unlock()
	if waiter == nil {
		return
	}

	select {
	case waiter <- struct{}{}:
	default:
	}
}
