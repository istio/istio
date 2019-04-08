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
	"strconv"
	"time"

	"github.com/gogo/protobuf/types"

	"go.opencensus.io/tag"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/log"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/converter"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"

	coreV1 "k8s.io/api/core/v1"
)

var (
	scope      = log.Scope
	collection = metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection

	// Schema for types required to generate synthetic ServiceEntry projections.
	Schema *resource.Schema
)

func init() {
	b := resource.NewSchemaBuilder()
	b.RegisterInfo(metadata.K8sCoreV1Pods)
	b.RegisterInfo(metadata.K8sCoreV1Nodes)
	b.RegisterInfo(metadata.K8sCoreV1Services)
	b.RegisterInfo(metadata.K8sCoreV1Endpoints)
	Schema = b.Build()
}

var _ processing.Handler = &Handler{}

// Handler is a processing.Handler that generates snapshots containing synthetic ServiceEntry projections.
type Handler struct {
	converter *converter.Instance

	listener processing.Listener

	services  map[resource.FullName]resource.Entry
	endpoints map[resource.FullName]resource.Entry
	ipToName  map[string]map[resource.FullName]struct{}

	podHandler  processing.Handler
	nodeHandler processing.Handler

	// The version number for the current State of the object. Every time mcpResources or versions change,
	// the version number also change
	version      int64
	mcpResources map[resource.FullName]*mcp.Resource

	// pendingEvents counts the number of events awaiting publishing.
	pendingEvents int64

	// lastSnapshotTime records the last time a snapshot was published.
	lastSnapshotTime time.Time

	statsCtx context.Context
}

// NewHandler creates a new Handler instance.
func NewHandler(domain string, listener processing.Listener) *Handler {
	handler := &Handler{
		listener:     listener,
		services:     make(map[resource.FullName]resource.Entry),
		endpoints:    make(map[resource.FullName]resource.Entry),
		ipToName:     make(map[string]map[resource.FullName]struct{}),
		mcpResources: make(map[resource.FullName]*mcp.Resource),
	}

	podCache, cacheHandler := pod.NewCache(pod.Listener{
		PodAdded:   handler.podUpdated,
		PodUpdated: handler.podUpdated,
		PodDeleted: handler.podUpdated,
	})

	handler.podHandler = cacheHandler
	handler.nodeHandler = cacheHandler

	handler.converter = converter.New(domain, podCache)

	statsCtx, err := tag.New(context.Background(), tag.Insert(monitoring.CollectionTag,
		metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection.String()))
	if err != nil {
		log.Scope.Errorf("Error creating monitoring context for counting state: %v", err)
		statsCtx = nil
	}
	handler.statsCtx = statsCtx

	return handler
}

// Handle incoming events and generate synthetic ServiceEntry projections.
func (h *Handler) Handle(event resource.Event) {
	switch event.Entry.ID.Collection {
	case metadata.K8sCoreV1Endpoints.Collection:
		// Update the projections
		h.handleEndpointsEvent(event)
	case metadata.K8sCoreV1Services.Collection:
		// Update the projections
		h.handleServiceEvent(event)
	case metadata.K8sCoreV1Nodes.Collection:
		// Update the pod cache.
		h.nodeHandler.Handle(event)
	case metadata.K8sCoreV1Pods.Collection:
		// Update the pod cache.
		h.podHandler.Handle(event)
	default:
		scope.Warnf("received event with unexpected collection: %v", event.Entry.ID.Collection)
	}
}

func (h *Handler) podUpdated(p pod.Info) {
	// Update the endpoints associated with this IP.
	for name := range h.ipToName[p.IP] {
		h.doUpdate(name)
	}
}

// Builds the snapshot of the current resources.
func (h *Handler) BuildSnapshot() snapshot.Snapshot {
	now := time.Now()
	monitoring.RecordProcessorSnapshotPublished(h.pendingEvents, now.Sub(h.lastSnapshotTime))
	h.lastSnapshotTime = now
	h.pendingEvents = 0

	b := snapshot.NewInMemoryBuilder()

	// Copy the entries.
	entries := make([]*mcp.Resource, 0, len(h.mcpResources))
	for _, r := range h.mcpResources {
		entries = append(entries, r)
	}

	// Create the collection resources on the Snapshot.
	version := strconv.FormatInt(h.version, 10)
	b.Set(collection.String(), version, entries)

	return b.Build()
}

func (h *Handler) handleServiceEvent(event resource.Event) {
	service := event.Entry
	name := service.ID.FullName

	switch event.Kind {
	case resource.Added, resource.Updated:
		// Store the service.
		h.services[name] = service

		h.doUpdate(name)
	case resource.Deleted:
		// Delete the Service and ServiceEntry
		delete(h.services, name)
		h.deleteMcpResource(name)
		h.updateVersion()
	default:
		scope.Errorf("unknown event kind: %v", event.Kind)
	}
}

func (h *Handler) handleEndpointsEvent(event resource.Event) {
	endpoints := event.Entry
	name := endpoints.ID.FullName

	switch event.Kind {
	case resource.Added, resource.Updated:
		// Update the IPs for this endpoint.
		h.updateEndpointIPs(name, endpoints)

		// Store the endpoints.
		h.endpoints[name] = endpoints

		h.doUpdate(name)
	case resource.Deleted:
		// Remove the IPs for this endpoint.
		h.deleteEndpointIPs(name, endpoints)

		// The lifecycle of the ServiceEntry is bound to the service, so only delete the endpoints entry here.
		delete(h.endpoints, name)

		h.doUpdate(name)
	default:
		scope.Errorf("unknown event kind: %v", event.Kind)
	}
}

func (h *Handler) setMcpEntry(name resource.FullName, mcpEntry *mcp.Resource) {
	h.mcpResources[name] = mcpEntry
}

func (h *Handler) deleteMcpResource(name resource.FullName) {
	delete(h.mcpResources, name)
}

func (h *Handler) updateVersion() {
	h.version++
	monitoring.RecordStateTypeCountWithContext(h.statsCtx, len(h.mcpResources))

	if scope.DebugEnabled() {
		scope.Debugf("in-memory state has changed:\n%v\n", h)
	}
	h.pendingEvents++
	h.notifyChanged()
}

func (h *Handler) notifyChanged() {
	h.listener.CollectionChanged(collection)
}

func (h *Handler) versionString() string {
	return strconv.FormatInt(h.version, 10)
}

func (h *Handler) doUpdate(name resource.FullName) {
	// Look up the service associated with the endpoints.
	service, ok := h.services[name]
	if !ok {
		// No service, nothing to update.
		return
	}

	// Get the associated endpoints, if available.
	var endpoints *resource.Entry
	if epEntry, ok := h.endpoints[name]; ok {
		endpoints = &epEntry
	}

	// Convert to an MCP resource to be used in the snapshot.
	mcpEntry, ok := h.toMcpResource(&service, endpoints)
	if !ok {
		return
	}
	h.setMcpEntry(name, mcpEntry)

	h.updateVersion()
}

func (h *Handler) toMcpResource(service *resource.Entry, endpoints *resource.Entry) (*mcp.Resource, bool) {
	meta := mcp.Metadata{}
	se := networking.ServiceEntry{}
	if err := h.converter.Convert(service, endpoints, &meta, &se); err != nil {
		scope.Errorf("error converting to ServiceEntry: %v", err)
		return nil, false
	}

	// Set the version on the metadata.
	meta.Version = h.versionString()

	body, err := types.MarshalAny(&se)
	if err != nil {
		scope.Errorf("error serializing proto from source e: %v", se)
		return nil, false
	}

	entry := &mcp.Resource{
		Metadata: &meta,
		Body:     body,
	}

	return entry, true
}

func (h *Handler) updateEndpointIPs(name resource.FullName, newRE resource.Entry) {
	newIPs := getEndpointIPs(newRE)
	var prevIPs map[string]struct{}

	if prev, exists := h.endpoints[name]; exists {
		prevIPs = getEndpointIPs(prev)

		// Delete any IPs missing from the new endpoints.
		for prevIP := range prevIPs {
			if _, exists := newIPs[prevIP]; !exists {
				h.deleteEndpointIP(name, prevIP)
			}
		}
	}

	// Add/update
	for newIP := range newIPs {
		names := h.ipToName[newIP]
		if names == nil {
			names = make(map[resource.FullName]struct{})
			h.ipToName[newIP] = names
		}
		names[name] = struct{}{}
	}
}

func (h *Handler) deleteEndpointIPs(name resource.FullName, endpoints resource.Entry) {
	ips := getEndpointIPs(endpoints)
	for ip := range ips {
		h.deleteEndpointIP(name, ip)
	}
}

func (h *Handler) deleteEndpointIP(name resource.FullName, ip string) {
	names := h.ipToName[ip]
	if names != nil {
		// Remove the name from the names map for this IP.
		delete(names, name)
		if len(names) == 0 {
			// There are no more endpoints using this IP. Delete the map.
			delete(h.ipToName, ip)
		}
	}
}

func getEndpointIPs(entry resource.Entry) map[string]struct{} {
	ips := make(map[string]struct{})
	endpoints := entry.Item.(*coreV1.Endpoints)
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			ips[address.IP] = struct{}{}
		}
	}
	return ips
}
