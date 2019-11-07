// Copyright 2019 Istio Authors
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

package serviceentry

import (
	"context"
	"fmt"
	"strconv"

	"go.opencensus.io/tag"
	coreV1 "k8s.io/api/core/v1"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/monitoring"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry/converter"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry/pod"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/scope"
)

type serviceEntryTransformer struct {
	inputs  collection.Names
	outputs collection.Names
	options processing.ProcessorOptions

	converter *converter.Instance

	services  map[resource.Name]*resource.Entry
	endpoints map[resource.Name]*resource.Entry
	ipToName  map[string]map[resource.Name]struct{}

	podHandler  event.Handler
	nodeHandler event.Handler

	handler  event.Handler
	statsCtx context.Context

	fullSyncCtr int

	// The version number for the current State of the object. Every time mcpResources or versions change,
	// the version number also changes
	version int64
}

var _ event.Transformer = &serviceEntryTransformer{}

// Start implements event.Transformer
func (t *serviceEntryTransformer) Start() {
	t.ipToName = make(map[string]map[resource.Name]struct{})
	t.services = make(map[resource.Name]*resource.Entry)
	t.endpoints = make(map[resource.Name]*resource.Entry)

	podCache, cacheHandler := pod.NewCache(pod.Listener{
		PodAdded:   t.podUpdated,
		PodUpdated: t.podUpdated,
		PodDeleted: t.podUpdated,
	})

	t.podHandler = cacheHandler
	t.nodeHandler = cacheHandler

	t.converter = converter.New(t.options.DomainSuffix, podCache)

	statsCtx, err := tag.New(context.Background(), tag.Insert(monitoring.CollectionTag,
		metadata.IstioNetworkingV1Alpha3SyntheticServiceentries.String()))
	if err != nil {
		scope.Processing.Errorf("Error creating monitoring context for counting state: %v", err)
		statsCtx = nil
	}
	t.statsCtx = statsCtx

	t.fullSyncCtr = len(t.Inputs())
}

// Stop implements event.Transformer
func (t *serviceEntryTransformer) Stop() {
	t.ipToName = nil
	t.services = nil
	t.endpoints = nil
}

// DispatchFor implements event.Transformer
func (t *serviceEntryTransformer) DispatchFor(c collection.Name, h event.Handler) {
	switch c {
	case metadata.IstioNetworkingV1Alpha3SyntheticServiceentries:
		t.handler = event.CombineHandlers(t.handler, h)
	}
}

// Inputs implements event.Transformer
func (t *serviceEntryTransformer) Inputs() collection.Names {
	return t.inputs
}

// Outputs implements event.Transformer
func (t *serviceEntryTransformer) Outputs() collection.Names {
	return t.outputs
}

// Handle implements event.Transformer
func (t *serviceEntryTransformer) Handle(e event.Event) {
	switch e.Kind {
	case event.FullSync:
		t.fullSyncCtr--
		if t.fullSyncCtr == 0 {
			t.dispatch(event.FullSyncFor(t.Outputs()[0]))
		}
		return

	case event.Reset:
		t.dispatch(event.Event{Kind: event.Reset})
		return

	case event.Added, event.Updated, event.Deleted:
		// fallthrough

	default:
		panic(fmt.Errorf("transformer.Handle: Unexpected event received: %v", e))
	}

	switch e.Source {
	case metadata.K8SCoreV1Endpoints:
		// Update the projections
		t.handleEndpointsEvent(e)
	case metadata.K8SCoreV1Services:
		// Update the projections
		t.handleServiceEvent(e)
	case metadata.K8SCoreV1Nodes:
		// Update the pod cache.
		t.nodeHandler.Handle(e)
	case metadata.K8SCoreV1Pods:
		// Update the pod cache.
		t.podHandler.Handle(e)
	default:
		panic(fmt.Errorf("received event with unexpected collection: %v", e.Source))
	}
}

func (t *serviceEntryTransformer) handleEndpointsEvent(e event.Event) {
	endpoints := e.Entry
	name := e.Entry.Metadata.Name

	switch e.Kind {
	case event.Added, event.Updated:
		// Update the IPs for this endpoint.
		t.updateEndpointIPs(name, endpoints)

		// Store the endpoints.
		t.endpoints[name] = endpoints

		t.doUpdate(name)
	case event.Deleted:
		// Remove the IPs for this endpoint.
		t.deleteEndpointIPs(name, endpoints)

		// The lifecycle of the ServiceEntry is bound to the service, so only delete the endpoints entry here.
		delete(t.endpoints, name)

		t.doUpdate(name)
	default:
		panic(fmt.Errorf("unknown event kind: %v", e.Kind))
	}
}

func (t *serviceEntryTransformer) handleServiceEvent(e event.Event) {
	service := e.Entry
	name := e.Entry.Metadata.Name

	switch e.Kind {
	case event.Added, event.Updated:
		// Store the service.
		t.services[name] = service

		t.doUpdate(name)
	case event.Deleted:
		// Delete the Service and ServiceEntry
		delete(t.services, name)
		t.sendDelete(name)

	default:
		panic(fmt.Errorf("unknown event kind: %v", e.Kind))
	}
}

func (t *serviceEntryTransformer) doUpdate(name resource.Name) {
	// Look up the service associated with the endpoints.
	service, ok := t.services[name]
	if !ok {
		// No service, nothing to update.
		return
	}

	// Get the associated endpoints, if available.
	endpoints := t.endpoints[name]

	// Convert to an MCP resource to be used in the snapshot.
	mcpEntry, ok := t.toMcpResource(service, endpoints)
	if !ok {
		return
	}
	// TODO: Distinguish between add/update.
	t.sendUpdate(mcpEntry)
}

func (t *serviceEntryTransformer) dispatch(e event.Event) {
	if t.handler != nil {
		t.handler.Handle(e)
	}
}

func (t *serviceEntryTransformer) sendDelete(name resource.Name) {
	e := event.Event{
		Kind:   event.Deleted,
		Source: metadata.IstioNetworkingV1Alpha3SyntheticServiceentries,
		Entry: &resource.Entry{
			Metadata: resource.Metadata{
				Name: name,
			},
		},
	}

	t.dispatch(e)
}

func (t *serviceEntryTransformer) sendUpdate(r *resource.Entry) {
	e := event.Event{
		Kind:   event.Updated,
		Source: metadata.IstioNetworkingV1Alpha3SyntheticServiceentries,
		Entry:  r,
	}

	t.dispatch(e)
}

func (t *serviceEntryTransformer) podUpdated(p pod.Info) {
	// Update the endpoints associated with this IP.
	for name := range t.ipToName[p.IP] {
		t.doUpdate(name)
	}
}

func (t *serviceEntryTransformer) updateEndpointIPs(name resource.Name, newRE *resource.Entry) {
	newIPs := getEndpointIPs(newRE)
	var prevIPs map[string]struct{}

	if prev, exists := t.endpoints[name]; exists {
		prevIPs = getEndpointIPs(prev)

		// Delete any IPs missing from the new endpoints.
		for prevIP := range prevIPs {
			if _, exists := newIPs[prevIP]; !exists {
				t.deleteEndpointIP(name, prevIP)
			}
		}
	}

	// Add/update
	for newIP := range newIPs {
		names := t.ipToName[newIP]
		if names == nil {
			names = make(map[resource.Name]struct{})
			t.ipToName[newIP] = names
		}
		names[name] = struct{}{}
	}
}

func (t *serviceEntryTransformer) deleteEndpointIPs(name resource.Name, endpoints *resource.Entry) {
	ips := getEndpointIPs(endpoints)
	for ip := range ips {
		t.deleteEndpointIP(name, ip)
	}
}

func (t *serviceEntryTransformer) deleteEndpointIP(name resource.Name, ip string) {
	if names := t.ipToName[ip]; names != nil {
		// Remove the name from the names map for this IP.
		delete(names, name)
		if len(names) == 0 {
			// There are no more endpoints using this IP. Delete the map.
			delete(t.ipToName, ip)
		}
	}
}

func getEndpointIPs(entry *resource.Entry) map[string]struct{} {
	ips := make(map[string]struct{})
	endpoints := entry.Item.(*coreV1.Endpoints)
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			ips[address.IP] = struct{}{}
		}
	}
	return ips
}

func (t *serviceEntryTransformer) toMcpResource(service *resource.Entry, endpoints *resource.Entry) (*resource.Entry, bool) {
	meta := resource.Metadata{
		Annotations: make(map[string]string),
		Labels:      make(map[string]string),
	}
	se := networking.ServiceEntry{}
	if err := t.converter.Convert(service, endpoints, &meta, &se); err != nil {
		scope.Processing.Errorf("error converting to ServiceEntry: %v", err)
		return nil, false
	}

	// Set the version on the metadata.
	meta.Version = resource.Version(t.versionString())

	entry := &resource.Entry{
		Metadata: meta,
		Item:     &se,
		Origin:   service.Origin,
	}
	return entry, true
}

func (t *serviceEntryTransformer) versionString() string {
	t.version++
	return strconv.FormatInt(t.version, 10)
}
