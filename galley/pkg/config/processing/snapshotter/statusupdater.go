package snapshotter

import (
	"sync"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

// StatusUpdater updates resource statuses, based on the given diagnostic messages.
type StatusUpdater interface {
	Update(messages diag.Messages)
}

// InMemoryStatusUpdater is an in-memory implementation of StatusUpdater
type InMemoryStatusUpdater struct {
	mu     sync.RWMutex
	m      diag.Messages
	waitCh chan struct{}
}

var _ StatusUpdater = &InMemoryStatusUpdater{}

// Update implements StatusUpdater
func (u *InMemoryStatusUpdater) Update(m diag.Messages) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.m = m
	if u.waitCh != nil {
		close(u.waitCh)
	}
}

// Get returns the current set of captured diag.Messages
func (u *InMemoryStatusUpdater) Get() diag.Messages {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.m
}

// WaitForReport blocks until a report is available. Returns true if a report is available, false if cancelCh was closed.
func (u *InMemoryStatusUpdater) WaitForReport(cancelCh chan struct{}) bool {
	u.mu.Lock()
	if u.m != nil {
		u.mu.Unlock()
		return true
	}

	if u.waitCh == nil {
		u.waitCh = make(chan struct{})
	}
	ch := u.waitCh
	u.mu.Unlock()

	select {
	case <-cancelCh:
		return false
	case <-ch:
		return true
	}
}
