package service

import (
	"sync"

	"github.com/google/uuid"
)

type JobNotifier interface {
	Subscribe(jobID uuid.UUID) (<-chan struct{}, func())
	Notify(jobID uuid.UUID)
}

type LocalJobNotifier struct {
	mu      sync.Mutex
	waiters map[uuid.UUID]chan struct{}
}

func NewLocalJobNotifier() *LocalJobNotifier {
	return &LocalJobNotifier{
		waiters: make(map[uuid.UUID]chan struct{}),
	}
}

func (n *LocalJobNotifier) Subscribe(jobID uuid.UUID) (<-chan struct{}, func()) {
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

func (n *LocalJobNotifier) Notify(jobID uuid.UUID) {
	n.mu.Lock()
	waiter := n.waiters[jobID]
	delete(n.waiters, jobID)
	n.mu.Unlock()
	if waiter == nil {
		return
	}

	select {
	case waiter <- struct{}{}:
	default:
	}
}
