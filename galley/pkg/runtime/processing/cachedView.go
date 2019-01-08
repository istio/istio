//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	"sync"
	"sync/atomic"

	"istio.io/istio/galley/pkg/runtime/resource"
	mcp "istio.io/api/mcp/v1alpha1"
)

type CachedView struct {
	typeURL resource.TypeURL
	generation int64

	entries map[resource.FullName]*mcp.Envelope
	entriesMutex sync.Mutex

	l ViewListener
}

var _ View = &CachedView{}

func NewCachedView(typeURL resource.TypeURL) *CachedView {
	return &CachedView{
		typeURL: typeURL,
		entries: make(map[resource.FullName]*mcp.Envelope),
	}
}

func (e *CachedView) Set(name resource.FullName, r *mcp.Envelope) {
	if e.set(name, r) {
		e.notify()
	}
}

func (e *CachedView) Remove(name resource.FullName) {
	if e.remove(name) {
		e.notify()
	}
}

func (e *CachedView) remove(name resource.FullName) bool {
	e.entriesMutex.Lock()
	defer e.entriesMutex.Unlock()

	_, changed := e.entries[name]
	delete(e.entries, name)

	if changed {
		atomic.AddInt64(&e.generation, 1)
	}
	return changed
}

func (e *CachedView) set(name resource.FullName, r *mcp.Envelope) bool {
	e.entriesMutex.Lock()
	defer e.entriesMutex.Unlock()

	old, found := e.entries[name]
	changed := found && old.Metadata.Version == r.Metadata.Version
	e.entries[name] = r

	if changed {
		atomic.AddInt64(&e.generation, 1)
	}

	return changed
}

func (e *CachedView) notify() {
	if e.l != nil {
		e.l.ViewChanged(e)
	}
}

// Type of the resources in this view.
func (e *CachedView) Type() resource.TypeURL {
	return e.typeURL
}

// Generation is the unique id of the current state of view. Everytime the state changes, the
// generation is updated.
func (e *CachedView) Generation() int64 {
	return atomic.LoadInt64(&e.generation)
}

// Get returns entries in this view
func (e *CachedView) Get() []*mcp.Envelope {
	result := make([]*mcp.Envelope, 0, len(e.entries))

	for _, v := range e.entries {
		result = append(result, v)
	}

	return result
}

func (e *CachedView) SetViewListener(l ViewListener) {
	e.l = l
}
