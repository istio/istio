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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/conversions"
	"istio.io/istio/galley/pkg/runtime/log"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

var _ processing.Handler = &State{}

// State is the in-memory state of Galley.
type State struct {
	schema   *resource.Schema
	listener processing.Listener

	config *Config

	// version counter is a nonce that generates unique ids for each updated view of State.
	versionCounter int64

	// entries for per-message-type State.
	entriesLock sync.Mutex
	entries     map[resource.Collection]*resourceTypeState

	// Virtual version numbers for Gateways & VirtualServices for Ingress projected ones
	ingressGWVersion   int64
	ingressVSVersion   int64
	lastIngressVersion int64

	// pendingEvents counts the number of events awaiting publishing.
	pendingEvents int64

	// lastSnapshotTime records the last time a snapshot was published.
	lastSnapshotTime time.Time
}

// per-resource-type State.
type resourceTypeState struct {
	// The version number for the current State of the object. Every time entries or versions change,
	// the version number also change
	version  int64
	entries  map[resource.FullName]*mcp.Resource
	versions map[resource.FullName]resource.Version
}

func newState(schema *resource.Schema, cfg *Config, listener processing.Listener) *State {
	now := time.Now()
	s := &State{
		schema:           schema,
		listener:         listener,
		config:           cfg,
		entries:          make(map[resource.Collection]*resourceTypeState),
		lastSnapshotTime: now,
	}

	// pre-populate state for all known types so that built snapshots
	// includes valid default version for empty resource collections.
	for _, info := range schema.All() {
		s.entries[info.Collection] = &resourceTypeState{
			entries:  make(map[resource.FullName]*mcp.Resource),
			versions: make(map[resource.FullName]resource.Version),
		}
	}

	return s
}

// Handle implements the processing.Handler interface.
func (s *State) Handle(event resource.Event) {
	pks, found := s.getResourceTypeState(event.Entry.ID.Collection)
	if !found {
		return
	}

	switch event.Kind {
	case resource.Added, resource.Updated:

		// Check to see if the version has changed.
		if curVersion := pks.versions[event.Entry.ID.FullName]; curVersion == event.Entry.ID.Version {
			log.Scope.Debugf("Received event for the current, known version: %v", event)
			return
		}

		// TODO: Check for content-wise equality

		entry, ok := s.toResource(event.Entry)
		if !ok {
			return
		}

		pks.entries[event.Entry.ID.FullName] = entry
		pks.versions[event.Entry.ID.FullName] = event.Entry.ID.Version
		monitoring.RecordStateTypeCount(event.Entry.ID.Collection.String(), len(pks.entries))

	case resource.Deleted:
		delete(pks.entries, event.Entry.ID.FullName)
		delete(pks.versions, event.Entry.ID.FullName)
		monitoring.RecordStateTypeCount(event.Entry.ID.Collection.String(), len(pks.entries))

	default:
		log.Scope.Errorf("Unknown event kind: %v", event.Kind)
		return
	}

	s.versionCounter++
	pks.version = s.versionCounter

	log.Scope.Debugf("In-memory State has changed:\n%v\n", s)
	s.pendingEvents++
	s.listener.CollectionChanged(event.Entry.ID.Collection)
}

func (s *State) getResourceTypeState(name resource.Collection) (*resourceTypeState, bool) {
	s.entriesLock.Lock()
	defer s.entriesLock.Unlock()

	pks, found := s.entries[name]
	return pks, found
}

func (s *State) buildSnapshot() snapshot.Snapshot {
	s.entriesLock.Lock()
	defer s.entriesLock.Unlock()

	now := time.Now()
	monitoring.RecordProcessorSnapshotPublished(s.pendingEvents, now.Sub(s.lastSnapshotTime))
	s.lastSnapshotTime = now

	b := snapshot.NewInMemoryBuilder()

	for collection, state := range s.entries {
		entries := make([]*mcp.Resource, 0, len(state.entries))
		for _, entry := range state.entries {
			entries = append(entries, entry)
		}
		version := fmt.Sprintf("%d", state.version)
		b.Set(collection.String(), version, entries)
	}

	// Build entities that are derived from existing ones.
	s.buildProjections(b)

	sn := b.Build()
	s.pendingEvents = 0
	return sn
}

func (s *State) buildProjections(b *snapshot.InMemoryBuilder) {
	s.buildIngressProjectionResources(b)
}

func (s *State) buildIngressProjectionResources(b *snapshot.InMemoryBuilder) {
	ingressByHost := make(map[string]resource.Entry)

	// Build ingress projections
	state := s.entries[metadata.K8sExtensionsV1beta1Ingresses.Collection]
	if state == nil || len(state.entries) == 0 {
		return
	}

	if s.lastIngressVersion != state.version {
		// Ingresses has changed
		s.versionCounter++
		s.ingressGWVersion = s.versionCounter
		s.versionCounter++
		s.ingressVSVersion = s.versionCounter
		s.lastIngressVersion = state.version
	}

	versionStr := fmt.Sprintf("%d_%d",
		s.entries[metadata.IstioNetworkingV1alpha3Gateways.Collection].version, s.ingressGWVersion)
	b.SetVersion(metadata.IstioNetworkingV1alpha3Gateways.Collection.String(), versionStr)

	versionStr = fmt.Sprintf("%d_%d",
		s.entries[metadata.IstioNetworkingV1alpha3Virtualservices.Collection].version, s.ingressVSVersion)
	b.SetVersion(metadata.IstioNetworkingV1alpha3Virtualservices.Collection.String(), versionStr)

	// Order names for stable generation.
	var orderedNames []resource.FullName
	for name := range state.entries {
		orderedNames = append(orderedNames, name)
	}
	sort.Slice(orderedNames, func(i, j int) bool {
		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
	})

	for _, name := range orderedNames {
		entry := state.entries[name]

		ingress, err := conversions.ToIngressSpec(entry)
		if err != nil {
			// Shouldn't happen
			log.Scope.Errorf("error during ingress projection: %v", err)
			continue
		}

		key := extractKey(name, state.versions[name])
		meta := extractMetadata(entry)

		conversions.IngressToVirtualService(key, meta, ingress, s.config.DomainSuffix, ingressByHost)

		gw := conversions.IngressToGateway(key, meta, ingress)

		err = b.SetEntry(
			metadata.IstioNetworkingV1alpha3Gateways.Collection.String(),
			gw.ID.FullName.String(),
			string(gw.ID.Version),
			gw.Metadata.CreateTime,
			nil,
			nil,
			gw.Item)
		if err != nil {
			log.Scope.Errorf("Unable to set gateway entry: %v", err)
		}
	}

	for _, e := range ingressByHost {
		err := b.SetEntry(
			metadata.IstioNetworkingV1alpha3Virtualservices.Collection.String(),
			e.ID.FullName.String(),
			string(e.ID.Version),
			e.Metadata.CreateTime,
			nil,
			nil,
			e.Item)
		if err != nil {
			log.Scope.Errorf("Unable to set virtualservice entry: %v", err)
		}
	}
}

func extractKey(name resource.FullName, version resource.Version) resource.VersionedKey {
	return resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata.K8sExtensionsV1beta1Ingresses.Collection,
			FullName:   name,
		},
		Version: version,
	}
}

func extractMetadata(entry *mcp.Resource) resource.Metadata {
	ts, err := types.TimestampFromProto(entry.Metadata.CreateTime)
	if err != nil {
		// It is an invalid timestamp. This shouldn't happen.
		log.Scope.Errorf("Error converting proto timestamp to time.Time: %v", err)
	}

	return resource.Metadata{
		CreateTime:  ts,
		Labels:      entry.Metadata.GetLabels(),
		Annotations: entry.Metadata.GetAnnotations(),
	}
}

func (s *State) toResource(e resource.Entry) (*mcp.Resource, bool) {
	body, err := types.MarshalAny(e.Item)
	if err != nil {
		log.Scope.Errorf("Error serializing proto from source e: %v:", e)
		return nil, false
	}

	createTime, err := types.TimestampProto(e.Metadata.CreateTime)
	if err != nil {
		log.Scope.Errorf("Error parsing resource create_time for event (%v): %v", e, err)
		return nil, false
	}

	entry := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:        e.ID.FullName.String(),
			CreateTime:  createTime,
			Version:     string(e.ID.Version),
			Labels:      e.Metadata.Labels,
			Annotations: e.Metadata.Annotations,
		},
		Body: body,
	}

	return entry, true
}

// String implements fmt.Stringer
func (s *State) String() string {
	var b bytes.Buffer

	_, _ = fmt.Fprintf(&b, "[State @%v]\n", s.versionCounter)

	sn := s.buildSnapshot().(*snapshot.InMemory)
	_, _ = fmt.Fprintf(&b, "%v", sn)

	return b.String()
}
