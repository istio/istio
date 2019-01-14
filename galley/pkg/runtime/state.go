// Copyright 2018 Istio Authors
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

package runtime

import (
	"bytes"
	"fmt"
	"sync"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/processors/identity"
	"istio.io/istio/galley/pkg/runtime/processors/ingress"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

// State is the in-memory state of Galley.
type State struct {
	schema *resource.Schema

	config *Config

	pipeline processing.Graph

	lock sync.Mutex

	changeGen uint64
}

func newState(schema *resource.Schema, cfg *Config) *State {
	s := &State{
		schema: schema,
		config: cfg,
	}

	b := processing.NewGraphBuilder()
	for _, info := range schema.All() {

		if info.TypeURL == metadata.IngressSpec.TypeURL {
			// Skip IngressSpec. We will create a separate processing pipeline for it.
			continue
		}
		identity.AddProcessor(info.TypeURL, b)
	}

	// Add projection pipelines
	icfg := ingress.Config{
		DomainSuffix: cfg.DomainSuffix,
	}
	ingress.AddProcessor(&icfg, b)

	b.AddListener(s)

	s.pipeline = b.Build()

	return s
}

func (s *State) apply(event resource.Event) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	changeGen := s.changeGen
	s.pipeline.Handle(event)
	return changeGen != s.changeGen
}

func (s *State) buildSnapshot() snapshot.Snapshot {
	s.lock.Lock()
	defer s.lock.Unlock()

	// TODO: cleanup and avoid allocations here
	var urls []resource.TypeURL
	for _, i := range s.schema.All() {
		urls = append(urls, i.TypeURL)
	}

	return s.pipeline.Snapshot(urls)
}

// String implements fmt.Stringer
func (s *State) String() string {
	var b bytes.Buffer

	b.WriteString("[State]\n")

	sn := s.buildSnapshot().(*snapshot.InMemory)
	fmt.Fprintf(&b, "%v", sn)

	return b.String()
}

func (s *State) TypeChanged(t resource.TypeURL) {
	s.changeGen++
}
