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
		schema:  schema,
		config:  cfg,
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
	ingress.AddIngressPipeline(b)

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
//
//func (s *State) rebuildProjections() {
//	s.rebuildIngressProjections()
//}
//
//func (s *State) rebuildIngressProjections() {
//	ingressByHost := make(map[string]*resource.Entry)
//
//	// Build ingress projections
//	state := s.entries[metadata.IngressSpec.TypeURL]
//	if state == nil {
//		return
//	}
//
//	// Order names for stable generation.
//	var orderedNames []resource.FullName
//	for name := range state.entries {
//		orderedNames = append(orderedNames, name)
//	}
//	sort.Slice(orderedNames, func(i, j int) bool {
//		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
//	})
//
//	for _, name := range orderedNames {
//		entry := state.entries[name]
//
//		ingress, err := conversions.ToIngressSpec(entry)
//		key := extractKey(name, entry, state.versions[name])
//		if err != nil {
//			// Shouldn't happen
//			scope.Errorf("error during ingress projection: %v", err)
//			continue
//		}
//		conversions.IngressToVirtualService(key, ingress, s.config.DomainSuffix, ingressByHost)
//
//		gw := conversions.IngressToGateway(key, ingress)
//
//		gwState := s.entries[metadata.Gateway.TypeURL]
//		gwState.projections[gw.ID.FullName] = envelope
//		err = b.SetEntry(
//			metadata.Gateway.TypeURL.String(),
//			gw.ID.FullName.String(),
//			string(gw.ID.Version),
//			gw.ID.CreateTime,
//			gw.Item)
//		if err != nil {
//			scope.Errorf("Unable to set gateway entry: %v", err)
//		}
//
//		// TODO: This is borked
//		b.SetVersion(metadata.Gateway.TypeURL.String(), string(gw.ID.Version))
//	}
//
//	for _, e := range ingressByHost {
//		err := b.SetEntry(
//			metadata.VirtualService.TypeURL.String(),
//			e.ID.FullName.String(),
//			string(e.ID.Version),
//			e.ID.CreateTime,
//			e.Item)
//		if err != nil {
//			scope.Errorf("Unable to set virtualservice entry: %v", err)
//		}
//		// TODO: This is borked
//		b.SetVersion(metadata.VirtualService.TypeURL.String(), string(e.ID.Version))
//	}
//}

//// originTracker tracks origin-reference relationships between resources.
//type originTracker struct {
//	contributions map[resource.FullName]contribution
//}
//
//type contribution struct {
//
//}
//
//// projectionSources are source pointers for a given, generated projection resource.
//type projectionSources []resource.VersionedKey
//
//type

//func (s *State) buildProjections(b *snapshot.InMemoryBuilder) {
//	s.buildIngressProjectionResources(b)
//}
//
//func (s *State) buildIngressProjectionResources(b *snapshot.InMemoryBuilder) {
//	ingressByHost := make(map[string]*resource.Entry)
//
//	// Build ingress projections
//	state := s.entries[metadata.IngressSpec.TypeURL]
//	if state == nil {
//		return
//	}
//
//	var orderedNames []resource.FullName
//	for name := range state.entries {
//		orderedNames = append(orderedNames, name)
//	}
//	sort.Slice(orderedNames, func(i, j int) bool {
//		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
//	})
//
//	for _, name := range orderedNames {
//		entry := state.entries[name]
//
//		ingress, err := conversions.ToIngressSpec(entry)
//		key := extractKey(name, entry, state.versions[name])
//		if err != nil {
//			// Shouldn't happen
//			scope.Errorf("error during ingress projection: %v", err)
//			continue
//		}
//		conversions.IngressToVirtualService(key, ingress, s.config.DomainSuffix, ingressByHost)
//
//		gw := conversions.IngressToGateway(key, ingress)
//
//		err = b.SetEntry(
//			metadata.Gateway.TypeURL.String(),
//			gw.ID.FullName.String(),
//			string(gw.ID.Version),
//			gw.ID.CreateTime,
//			gw.Item)
//		if err != nil {
//			scope.Errorf("Unable to set gateway entry: %v", err)
//		}
//
//		// TODO: This is borked
//		b.SetVersion(metadata.Gateway.TypeURL.String(), string(gw.ID.Version))
//	}
//
//	for _, e := range ingressByHost {
//		err := b.SetEntry(
//			metadata.VirtualService.TypeURL.String(),
//			e.ID.FullName.String(),
//			string(e.ID.Version),
//			e.ID.CreateTime,
//			e.Item)
//		if err != nil {
//			scope.Errorf("Unable to set virtualservice entry: %v", err)
//		}
//		// TODO: This is borked
//		b.SetVersion(metadata.VirtualService.TypeURL.String(), string(e.ID.Version))
//	}
//}

//func extractKey(name resource.FullName, entry *mcp.Envelope, version resource.Version) resource.VersionedKey {
//	ts, err := types.TimestampFromProto(entry.Metadata.CreateTime)
//	if err != nil {
//		// It is an invalid timestamp. This shouldn't happen.
//		scope.Errorf("Error converting proto timestamp to time.Time: %v", err)
//	}
//
//	return resource.VersionedKey{
//		Key: resource.Key{
//			TypeURL:  metadata.IngressSpec.TypeURL,
//			FullName: name,
//		},
//		Version:    version,
//		CreateTime: ts,
//	}
//}
//
//func (s *State) envelopeResource(e resource.Entry) (*mcp.Envelope, bool) {
//	serialized, err := proto.Marshal(e.Item)
//	if err != nil {
//		scope.Errorf("Error serializing proto from source e: %v:", e)
//		return nil, false
//	}
//
//	createTime, err := types.TimestampProto(e.ID.CreateTime)
//	if err != nil {
//		scope.Errorf("Error parsing resource create_time for event (%v): %v", e, err)
//		return nil, false
//	}
//
//	entry := &mcp.Envelope{
//		Metadata: &mcp.Metadata{
//			Name:       e.ID.FullName.String(),
//			CreateTime: createTime,
//			Version:    string(e.ID.Version),
//		},
//		Resource: &types.Any{
//			TypeUrl: e.ID.TypeURL.String(),
//			Value:   serialized,
//		},
//	}
//
//	return entry, true
//}

// String implements fmt.Stringer
func (s *State) String() string {
	var b bytes.Buffer

	b.WriteString("[State]\n")

	sn := s.buildSnapshot().(*snapshot.InMemory)
	fmt.Fprintf(&b, "%v", sn)

	return b.String()
}
