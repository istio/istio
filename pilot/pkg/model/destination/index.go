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

package destination

import (
	"reflect"
	"sort"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
)

// Resolver turns a closed endpoint descriptor into the common endpoint model.
// The returned dependency keys must cover every non-KRT input read by the
// resolver. KRT collections read through ctx are tracked automatically.
type Resolver func(krt.HandlerContext, DestinationDefinition, DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey)

// ResolvedDestination is the active, endpoint-resolved object consumed by
// dataplane projections. Definitions with no authorized binding are inert.
type ResolvedDestination struct {
	Binding      DestinationBinding
	Definition   DestinationDefinition
	Endpoints    []*model.IstioEndpoint
	Dependencies []model.ConfigKey
}

func (r ResolvedDestination) ResourceName() string              { return r.Binding.ResourceName() }
func (r ResolvedDestination) Equals(o ResolvedDestination) bool { return reflect.DeepEqual(r, o) }

type IndexOptions struct {
	Resolvers map[EndpointSourceKind]Resolver
}

// Index owns the active, resolved destination collection and consumer lookup.
// Definitions without bindings never appear in Resolved.
type Index struct {
	Resolved krt.Collection[ResolvedDestination]

	registration krt.HandlerRegistration
	mu           sync.RWMutex
	byConsumer   map[ConsumerID][]ResolvedDestination
}

func NewIndex(
	definitions krt.Collection[DestinationDefinition],
	bindings krt.Collection[DestinationBinding],
	opts krt.OptionsBuilder,
	indexOpts IndexOptions,
) *Index {
	resolvers := builtinResolvers()
	for endpointKind, resolver := range indexOpts.Resolvers {
		resolvers[endpointKind] = resolver
	}
	definitionsByID := krt.NewIndex(definitions, "destination-definition", func(d DestinationDefinition) []DefinitionID {
		return []DefinitionID{d.ID}
	})
	resolved := krt.NewCollection(bindings, func(ctx krt.HandlerContext, binding DestinationBinding) *ResolvedDestination {
		definitions := definitionsByID.Fetch(ctx, binding.Definition)
		if len(definitions) != 1 {
			// A missing or ambiguous definition fails closed.
			return nil
		}
		definition := definitions[0]
		resolver := resolvers[definition.Endpoints.Kind]
		if resolver == nil {
			return nil
		}
		endpoints, resolverDependencies := resolver(ctx, definition, binding)
		return &ResolvedDestination{
			Binding: binding, Definition: definition, Endpoints: endpoints,
			Dependencies: NormalizeDependencies(append(
				append([]model.ConfigKey{definition.ID.Source}, binding.Dependencies...), resolverDependencies...,
			)...),
		}
	}, opts.WithName("ResolvedDestinations")...)
	idx := &Index{Resolved: resolved, byConsumer: map[ConsumerID][]ResolvedDestination{}}
	idx.registration = resolved.RegisterBatch(idx.rebuild, true)
	return idx
}

func builtinResolvers() map[EndpointSourceKind]Resolver {
	resolveAddress := func(_ krt.HandlerContext, definition DestinationDefinition, binding DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey) {
		source := binding.Endpoints
		portSpec := binding.Port
		if source.Hostname == "" || portSpec.Number < 1 || portSpec.Number > 65535 {
			return nil, nil
		}
		port := source.Port
		if port == 0 {
			port = uint32(portSpec.Number)
		}
		return []*model.IstioEndpoint{{
			Addresses: []string{source.Hostname.String()}, ServicePortName: portSpec.Name,
			EndpointPort: port, Namespace: definition.Namespace,
		}}, nil
	}
	return map[EndpointSourceKind]Resolver{
		DNS:        resolveAddress,
		DynamicDNS: resolveAddress,
	}
}

func (i *Index) rebuild(_ []krt.Event[ResolvedDestination]) {
	next := map[ConsumerID][]ResolvedDestination{}
	for _, destination := range i.Resolved.List() {
		next[destination.Binding.Consumer] = append(next[destination.Binding.Consumer], destination)
	}
	for consumer := range next {
		sort.Slice(next[consumer], func(a, b int) bool {
			return next[consumer][a].Binding.Key.String() < next[consumer][b].Binding.Key.String()
		})
	}
	i.mu.Lock()
	i.byConsumer = next
	i.mu.Unlock()
}

// ForConsumer returns a stable snapshot containing only explicitly authorized
// bindings for consumer. Returned endpoints are shallow-copied by value; callers
// must treat pointed-to endpoint objects as immutable.
func (i *Index) ForConsumer(consumer ConsumerID) []ResolvedDestination {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := append([]ResolvedDestination(nil), i.byConsumer[consumer]...)
	// ConsumerSet is intentionally pattern-based so waypoint/sidecar scopes do
	// not need expansion to every proxy identity.
	for bindingConsumer, destinations := range i.byConsumer {
		if bindingConsumer == consumer {
			continue
		}
		for _, destination := range destinations {
			visibility := destination.Binding.Visibility
			if visibility.Kind != "" && visibility.Kind == consumer.Kind &&
				(visibility.Namespace == "" || visibility.Namespace == consumer.Namespace) &&
				(visibility.Name == "" || visibility.Name == consumer.Name) {
				out = append(out, destination)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Binding.ResourceName() < out[b].Binding.ResourceName() })
	return out
}

func (i *Index) HasSynced() bool { return i.registration.HasSynced() }
