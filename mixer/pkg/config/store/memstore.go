// Copyright 2017 Istio Authors
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

package store

import (
	"context"
	"sync"
)

// Memstore is on-memory implementation of StoreBackend. Helpful for testing.
type Memstore struct {
	mu   sync.RWMutex
	data map[Key]*BackEndResource

	watchMutex sync.RWMutex
	watchCtx   context.Context
	watchCh    chan BackendEvent
}

func NewMemstore() *Memstore {
	return &Memstore{data: map[Key]*BackEndResource{}}
}

// Init implements StoreBackend interface.
func (m *Memstore) Init(ctx context.Context, kinds []string) error {
	return nil
}

// Watch implements StoreBackend interface.
func (m *Memstore) Watch(ctx context.Context) (<-chan BackendEvent, error) {
	ch := make(chan BackendEvent)
	m.watchMutex.Lock()
	m.watchCtx = ctx
	m.watchCh = ch
	m.watchMutex.Unlock()
	return ch, nil
}

// Get implements StoreBackend interface.
func (m *Memstore) Get(key Key) (*BackEndResource, error) {
	m.mu.RLock()
	v, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

// List implements StoreBackend interface.
func (m *Memstore) List() map[Key]*BackEndResource {
	m.mu.RLock()
	copied := make(map[Key]*BackEndResource, len(m.data))
	for k, v := range m.data {
		copied[k] = v
	}
	m.mu.RUnlock()
	return copied
}

// Put implements MemstoreWriter interface.
func (m *Memstore) Put(key Key, resource *BackEndResource) {
	m.mu.Lock()
	if resource.Metadata.Name == "" {
		resource.Metadata.Name = key.Name
	}
	if resource.Metadata.Namespace == "" {
		resource.Metadata.Namespace = key.Namespace
	}
	m.data[key] = resource
	m.mu.Unlock()

	m.watchMutex.RLock()
	if m.watchCh != nil {
		select {
		case <-m.watchCtx.Done():
		case m.watchCh <- BackendEvent{Type: Update, Key: key, Value: resource}:
		}
	}
	m.watchMutex.RUnlock()
}

// Delete implements MemstoreWriter interface.
func (m *Memstore) Delete(key Key) {
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()

	m.watchMutex.RLock()
	if m.watchCh != nil {
		select {
		case <-m.watchCtx.Done():
		case m.watchCh <- BackendEvent{Type: Delete, Key: key}:
		}
	}
	m.watchMutex.RUnlock()
}
