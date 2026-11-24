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
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
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

// WriteJSON writes the same JSON array as MarshalJSON to w, but streams it, so the full dump is never held in
// memory. It uses encoding/json/v2 with v1 options, so the output matches json.Marshal(p), or
// json.MarshalIndent(p, "", indent), apart from a trailing newline.
// ctx is checked between collections, so a cancelled request stops early.
func (p *DebugHandler) WriteJSON(ctx context.Context, w io.Writer, indent string) error {
	if p == nil {
		_, err := io.WriteString(w, "null")
		return err
	}
	// Do not hold the lock while dumping; the dumps can be slow on large collections.
	p.mu.RLock()
	collections := maps.Values(p.debugCollections)
	p.mu.RUnlock()

	opts := []jsonv2.Options{json.DefaultOptionsV1()}
	if indent != "" {
		opts = append(opts, jsontext.WithIndent(indent))
	}
	enc := jsontext.NewEncoder(w, opts...)
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, c := range collections {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Encode the fields directly, and dump each collection only when it is encoded.
		// Going through DebugCollection.MarshalJSON would buffer the whole collection.
		if err := jsonv2.MarshalEncode(enc, c.jsonFields(), opts...); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndArray)
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
	return json.Marshal(p.jsonFields())
}

func (p DebugCollection) jsonFields() map[string]any {
	return map[string]any{
		"uid":   p.uid,
		"name":  p.name,
		"state": p.dump(),
	}
}

// maybeRegisterCollectionForDebugging registers the collection in the debugger, if one is enabled
func maybeRegisterCollectionForDebugging[T any](c internalCollection[T], handler *DebugHandler) {
	if handler == nil {
		return
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.debugCollections == nil {
		handler.debugCollections = make(map[collectionUID]DebugCollection)
	}
	handler.debugCollections[c.uid()] = DebugCollection{
		name: c.name(),
		dump: c.dump,
		uid:  c.uid(),
	}
}

// maybeUnregisterCollectionFromDebugger removes the collection from the debugger, if one is enabled
func maybeUnregisterCollectionFromDebugger[T any](c internalCollection[T], handler *DebugHandler) {
	if handler == nil {
		return
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	delete(handler.debugCollections, c.uid())
}

// nolint: unused // (not true, not sure why it thinks it is!)
func eraseMap[T any](l map[Key[T]]T) map[string]any {
	nm := make(map[string]any, len(l))
	for k, v := range l {
		nm[string(k)] = v
	}
	return nm
}
