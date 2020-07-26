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

// Package storetest provides the utility functions of config store for
// testing. Shouldn't be imported outside of the test.
package storetest

import (
	"fmt"
	"strings"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/pkg/config/store"
)

// Memstore is an on-memory store backend. Used only for testing.
type Memstore struct {
	mu    sync.Mutex
	data  map[store.Key]*store.BackEndResource
	wch   chan store.BackendEvent
	donec chan struct{}
}

// NewMemstore creates a new Memstore instance.
func NewMemstore() *Memstore {
	return &Memstore{data: map[store.Key]*store.BackEndResource{}, donec: make(chan struct{})}
}

// Stop implements store.Backend interface.
func (m *Memstore) Stop() {
	close(m.donec)
}

// Init implements store.Backend interface.
func (m *Memstore) Init(kinds []string) error {
	return nil
}

// WaitForSynced implements store.Backend interface
func (m *Memstore) WaitForSynced(time.Duration) error {
	return nil
}

// Watch implements store.Backend interface.
func (m *Memstore) Watch() (<-chan store.BackendEvent, error) {
	// Watch is not supported in the memstore, but sometimes it needs to be invoked.
	c := make(chan store.BackendEvent)
	go func() {
		<-m.donec
		close(c)
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wch = c
	return c, nil
}

// Get implements store.Backend interface.
func (m *Memstore) Get(key store.Key) (*store.BackEndResource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.data[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	return r, nil
}

// List implements store.Backend interface.
func (m *Memstore) List() map[store.Key]*store.BackEndResource {
	return m.data
}

// Put adds a new resource to the memstore.
func (m *Memstore) Put(r *store.BackEndResource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[r.Key()] = r
	if m.wch != nil {
		m.wch <- store.BackendEvent{Type: store.Update, Key: r.Key(), Value: r}
	}
}

// Delete removes a resource for the specified key from the memstore.
func (m *Memstore) Delete(k store.Key) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, k)
	if m.wch != nil {
		m.wch <- store.BackendEvent{Type: store.Delete, Key: k}
	}
}

// SetupStoreForTest creates an on-memory store backend, initializes its
// data with the specified specs, and returns a new store with the backend.
// Note that this store can't change, Watch does not emit any events.
func SetupStoreForTest(data ...string) (store.Store, error) {
	m := NewMemstore()
	var errs error
	for i, d := range data {
		for j, chunk := range strings.Split(d, "\n---\n") {
			chunk = strings.TrimSpace(chunk)
			if len(chunk) == 0 {
				continue
			}
			r, err := store.ParseChunk([]byte(chunk))
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("failed to parse at %d/%d: %v", i, j, err))
				continue
			}
			if r == nil {
				continue
			}
			m.data[r.Key()] = r
		}
	}

	if errs != nil {
		return nil, errs
	}
	return store.WithBackend(m), nil
}
