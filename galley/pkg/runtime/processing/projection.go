//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain accumulator copy of the License at
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

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// Projection is an interface for collecting the resources for building a snapshot.
type Projection interface {
	// Type of the resources in this projection.
	Type() resource.TypeURL

	// Generation is the unique id of the current state of projection. Every time the underlying state
	// changes, the generation is updated.
	Generation() int64

	// Get returns entries in this projection
	Get() []*mcp.Resource

	SetProjectionListener(l ProjectionListener)
}

// ProjectionListener gets notified when a view changes.
type ProjectionListener interface {
	ProjectionChanged(p Projection)
}

type StoredProjection struct {
	typeURL    resource.TypeURL
	generation int64

	entries      map[resource.FullName]*mcp.Resource
	entriesMutex sync.Mutex

	l ProjectionListener
}

var _ Projection = &StoredProjection{}

func NewStoredProjection(typeURL resource.TypeURL) *StoredProjection {
	return &StoredProjection{
		typeURL: typeURL,
		entries: make(map[resource.FullName]*mcp.Resource),
	}
}

func (e *StoredProjection) Set(name resource.FullName, r *mcp.Resource) {
	if e.set(name, r) {
		atomic.AddInt64(&e.generation, 1)
		e.notify()
	}
}

func (e *StoredProjection) Remove(name resource.FullName) {
	if e.remove(name) {
		atomic.AddInt64(&e.generation, 1)
		e.notify()
	}
}

func (e *StoredProjection) remove(name resource.FullName) bool {
	e.entriesMutex.Lock()
	defer e.entriesMutex.Unlock()

	_, changed := e.entries[name]
	delete(e.entries, name)

	return changed
}

func (e *StoredProjection) set(name resource.FullName, r *mcp.Resource) bool {
	e.entriesMutex.Lock()
	defer e.entriesMutex.Unlock()

	old, found := e.entries[name]
	changed := !found || old.Metadata.Version != r.Metadata.Version
	e.entries[name] = r

	return changed
}

func (e *StoredProjection) notify() {
	if e.l != nil {
		e.l.ProjectionChanged(e)
	}
}

// Type implements Projection
func (e *StoredProjection) Type() resource.TypeURL {
	return e.typeURL
}

// Generation implements Projection
func (e *StoredProjection) Generation() int64 {
	return atomic.LoadInt64(&e.generation)
}

// Get implements Projection
func (e *StoredProjection) Get() []*mcp.Resource {
	result := make([]*mcp.Resource, 0, len(e.entries))

	for _, v := range e.entries {
		result = append(result, v)
	}

	return result
}

func (e *StoredProjection) SetProjectionListener(l ProjectionListener) {
	e.l = l
}
