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

package mcp

import (
	"fmt"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/mcp/sink"
)

var _ event.Source = &Source{}
var _ sink.Updater = &Source{}

// Source for events from an MCP endpoint.
type Source struct {
	caches map[string]*cache
}

// NewSource creates a new MCP event Source.
func NewSource(schemas collection.Schemas) *Source {
	all := schemas.All()
	caches := make(map[string]*cache, len(all))
	for _, c := range all {
		caches[c.Name().String()] = newCache(c)
	}

	return &Source{
		caches: caches,
	}
}

func (s Source) Apply(change *sink.Change) error {
	c := s.caches[change.Collection]
	if c == nil {
		return fmt.Errorf("failed applying change for unknown collection (%v)", change.Collection)
	}
	return c.apply(change)
}

func (s Source) Dispatch(handler event.Handler) {
	for _, c := range s.caches {
		c.Dispatch(handler)
	}
}

func (s Source) Start() {
	for _, c := range s.caches {
		c.Start()
	}
}

func (s Source) Stop() {
	for _, c := range s.caches {
		c.Stop()
	}
}
