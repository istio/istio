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
	"fmt"
	"sync"
)

const memstoreScheme = "memstore"

// memstoreMap creates the mapping between the memstore URL and the created
// instance, so tests can make updates on the memstore after initializations.
var memstoreMap = map[string]*memstore{}
var memstoreMapMutex sync.RWMutex

// memstore is on-memory implementation of Store2Backend. Helpful for testing.
type memstore struct {
	mu   sync.RWMutex
	data map[Key]*BackEndResource

	watchMutex sync.RWMutex
	watchCtx   context.Context
	watchCh    chan BackendEvent
}

func createMemstore(u fmt.Stringer) *memstore {
	m := &memstore{data: map[Key]*BackEndResource{}}
	memstoreMapMutex.Lock()
	memstoreMap[u.String()] = m
	memstoreMapMutex.Unlock()
	return m
}

// Init implements Store2Backend interface.
func (m *memstore) Init(ctx context.Context, kinds []string) error {
	return nil
}

// Watch implements Store2Backend interface.
func (m *memstore) Watch(ctx context.Context) (<-chan BackendEvent, error) {
	ch := make(chan BackendEvent)
	m.watchMutex.Lock()
	m.watchCtx = ctx
	m.watchCh = ch
	m.watchMutex.Unlock()
	return ch, nil
}

// Get implements Store2Backend interface.
func (m *memstore) Get(key Key) (*BackEndResource, error) {
	m.mu.RLock()
	v, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

// List implements Store2Backend interface.
func (m *memstore) List() map[Key]*BackEndResource {
	m.mu.RLock()
	copied := make(map[Key]*BackEndResource, len(m.data))
	for k, v := range m.data {
		copied[k] = v
	}
	m.mu.RUnlock()
	return copied
}

// Put implements MemstoreWriter interface.
func (m *memstore) Put(key Key, resource *BackEndResource) {
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
func (m *memstore) Delete(key Key) {
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

// MemstoreWriter is the interface to make changes on the memstore backend. This
// will be used by tests to set up the on-memory data in the store.
type MemstoreWriter interface {
	Put(key Key, resource *BackEndResource)
	Delete(key Key)
}

// GetMemstoreWriter returns the MemstoreWriter used for the config store URL, or nil
// if the URL is not used.
func GetMemstoreWriter(u string) MemstoreWriter {
	memstoreMapMutex.RLock()
	defer memstoreMapMutex.RUnlock()
	return memstoreMap[u]
}
