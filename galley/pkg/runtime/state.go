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
	"istio.io/istio/galley/pkg/runtime/conversions/envelope"
	"istio.io/istio/galley/pkg/runtime/conversions/ingress"
	"istio.io/istio/galley/pkg/runtime/processing"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

// State is the in-memory state of Galley.
type State struct {
	schema *resource.Schema

	config *Config

	pipeline processing.Pipeline

	lock sync.Mutex
}

func newState(schema *resource.Schema, cfg *Config) *State {
	s := &State{
		schema: schema,
		config: cfg,
	}

	b := processing.NewPipelineBuilder()
	for _, info := range schema.All() {

		if info.TypeURL == metadata.IngressSpec.TypeURL {
			// Skip IngressSpec. We will create a separate processing pipeline for it.
			continue
		}
		envelope.AddDirectEnvelopePipeline(info.TypeURL, b)
	}

	// Add projection pipelines
	icfg := ingress.Config{
		DomainSuffix: cfg.DomainSuffix,
	}
	ingress.AddIngressPipeline(&icfg, b)

	s.pipeline = b.Build()

	return s
}

func (s *State) apply(event resource.Event) bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.pipeline.Handle(event)
}

func (s *State) buildSnapshot() snapshot.Snapshot {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.pipeline.Snapshot()
}

// String implements fmt.Stringer
func (s *State) String() string {
	var b bytes.Buffer

	b.WriteString("[State]\n")

	sn := s.buildSnapshot().(*snapshot.InMemory)
	fmt.Fprintf(&b, "%v", sn)

	return b.String()
}
