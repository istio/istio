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

package krt

import (
	"encoding/json"
	"sync"

	"istio.io/istio/pkg/maps"
)

// DebugHandler allows attaching a variety of collections to it and then dumping them
type DebugHandler struct {
	debugCollections map[collectionUID]DebugCollection
	mu               sync.RWMutex
}

func (p *DebugHandler) MarshalJSON() ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return json.Marshal(maps.Values(p.debugCollections))
}

var GlobalDebugHandler = new(DebugHandler)

type CollectionDump struct {
	// Map of output key -> output
	Outputs map[string]any `json:"outputs,omitempty"`
	// Name of the input collection
	InputCollection string `json:"inputCollection,omitempty"`
	// Map of input key -> info
	Inputs map[string]InputDump `json:"inputs,omitempty"`
	// Synced returns whether the collection is synced or not
	Synced bool `json:"synced"`
}
type InputDump struct {
	Outputs      []string `json:"outputs,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}
type DebugCollection struct {
	name string
	dump func() CollectionDump
	uid  collectionUID
}

func (p DebugCollection) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"uid":   p.uid,
		"name":  p.name,
		"state": p.dump(),
	})
}

// maybeRegisterCollectionForDebugging registers the collection in the debugger, if one is enabled
func maybeRegisterCollectionForDebugging[T any](c Collection[T], handler *DebugHandler) {
	if handler == nil {
		return
	}
	cc := c.(internalCollection[T])
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.debugCollections == nil {
		handler.debugCollections = make(map[collectionUID]DebugCollection)
	}
	handler.debugCollections[cc.uid()] = DebugCollection{
		name: cc.name(),
		dump: cc.dump,
		uid:  cc.uid(),
	}
}

// maybeUnregisterCollectionFromDebugger removes the collection from the debugger, if one is enabled
func maybeUnregisterCollectionFromDebugger[T any](c Collection[T], handler *DebugHandler) {
	if handler == nil {
		return
	}
	cc := c.(internalCollection[T])
	handler.mu.Lock()
	defer handler.mu.Unlock()
	delete(handler.debugCollections, cc.uid())
}

// nolint: unused // (not true, not sure why it thinks it is!)
func eraseMap[T any](l map[Key[T]]T) map[string]any {
	nm := make(map[string]any, len(l))
	for k, v := range l {
		nm[string(k)] = v
	}
	return nm
}
