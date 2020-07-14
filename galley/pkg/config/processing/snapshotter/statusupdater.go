// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package snapshotter

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

// StatusUpdater updates resource statuses, based on the given diagnostic messages.
type StatusUpdater interface {
	Update(messages diag.Messages)
}

// InMemoryStatusUpdater is an in-memory implementation of StatusUpdater
type InMemoryStatusUpdater struct {
	WaitTimeout time.Duration

	mu      sync.RWMutex
	m       diag.Messages
	updated bool
	waitCh  chan struct{}
}

var _ StatusUpdater = &InMemoryStatusUpdater{}

// Update implements StatusUpdater
func (u *InMemoryStatusUpdater) Update(m diag.Messages) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.m = m

	// Why an explicit flag here? Go treats an empty slice as if it were nil, so if no messages ever got recorded "u.m == nil" evaluates to true,
	// and the short-circuit in WaitForReport wouldn't ever fire.
	u.updated = true

	if u.waitCh != nil {
		close(u.waitCh)
		u.waitCh = nil
	}
}

// Get returns the current set of captured diag.Messages
func (u *InMemoryStatusUpdater) Get() diag.Messages {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.m
}

// WaitForReport blocks until a report is available. Returns nil if a report is available, or an error representing why we couldn't get it.
func (u *InMemoryStatusUpdater) WaitForReport(cancelCh chan struct{}) error {
	// Short-circuit to handle the case where Update got called before WaitForReport
	u.mu.Lock()
	if u.updated {
		u.mu.Unlock()
		return nil
	}

	if u.waitCh == nil {
		u.waitCh = make(chan struct{})
	}
	ch := u.waitCh
	u.mu.Unlock()

	select {
	case <-cancelCh:
		return fmt.Errorf("cancelled")
	case <-time.After(u.WaitTimeout):
		return fmt.Errorf("timed out after %s", u.WaitTimeout)
	case <-ch:
		return nil
	}
}
