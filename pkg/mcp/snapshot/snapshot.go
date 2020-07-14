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

package snapshot

import (
	"sort"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/source"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// Snapshot provides an immutable view of versioned resources.
type Snapshot interface {
	Collections() []string
	Resources(collection string) []*mcp.Resource
	Version(collection string) string
}

// Info is used for configz
type Info struct {
	// Collection of mcp resource
	Collection string
	// Version of the resource
	Version string
	// Names of the resource entries.
	Names []string
	// Synced of this collection, including peerAddr and synced status
	Synced map[string]bool
}

// Cache is a snapshot-based cache that maintains a single versioned
// snapshot of responses per group of clients. Cache consistently replies with the
// latest snapshot.
type Cache struct {
	mu         sync.RWMutex
	snapshots  map[string]Snapshot
	status     map[string]*StatusInfo
	watchCount int64

	groupIndex GroupIndexFn
}

// GroupIndexFn returns a stable group index for the given MCP collection and node.
// This is how an MCP server partitions snapshots to different clients. The index
// function is an implementation detail of the MCP server and Istio does not
// depend on any particular mapping.
type GroupIndexFn func(collection string, node *mcp.SinkNode) string

// New creates a new cache of resource snapshots.
func New(groupIndex GroupIndexFn) *Cache {
	return &Cache{
		snapshots:  make(map[string]Snapshot),
		status:     make(map[string]*StatusInfo),
		groupIndex: groupIndex,
	}
}

var _ source.Watcher = &Cache{}

type responseWatch struct {
	request      *source.Request
	pushResponse source.PushResponseFunc
}

// StatusInfo records watch status information of a group.
type StatusInfo struct {
	mu                   sync.RWMutex
	lastWatchRequestTime time.Time // informational
	watches              map[int64]*responseWatch
	// the synced structure is {Collection: {peerAddress: synced|nosynced}}.
	synced map[string]map[string]bool
}

// Watches returns the number of open watches.
func (si *StatusInfo) Watches() int {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return len(si.watches)
}

// LastWatchRequestTime returns the time the most recent watch request
// was received.
func (si *StatusInfo) LastWatchRequestTime() time.Time {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.lastWatchRequestTime
}

// Watch returns a watch for an MCP request.
func (c *Cache) Watch(
	request *source.Request,
	pushResponse source.PushResponseFunc,
	peerAddr string) source.CancelWatchFunc {
	group := c.groupIndex(request.Collection, request.SinkNode)

	c.mu.Lock()
	defer c.mu.Unlock()

	info := c.fillStatus(group, request, peerAddr)

	collection := request.Collection

	// return an immediate response if a snapshot is available and the
	// requested version doesn't match.
	if snapshot, ok := c.snapshots[group]; ok {

		version := snapshot.Version(request.Collection)
		scope.Debugf("Found snapshot for group: %q for %v @ version: %q",
			group, request.Collection, version)

		if version != request.VersionInfo {
			scope.Debugf("Responding to group %q snapshot:\n%v\n", group, snapshot)
			response := &source.WatchResponse{
				Collection: request.Collection,
				Version:    version,
				Resources:  snapshot.Resources(request.Collection),
				Request:    request,
			}
			pushResponse(response)
			return nil
		}
		info.synced[request.Collection][peerAddr] = true
	}

	// Otherwise, open a watch if no snapshot was available or the requested version is up-to-date.
	c.watchCount++
	watchID := c.watchCount

	scope.Infof("Watch(): created watch %d for %s from group %q, version %q",
		watchID, collection, group, request.VersionInfo)

	info.mu.Lock()
	info.watches[watchID] = &responseWatch{request: request, pushResponse: pushResponse}
	info.mu.Unlock()

	cancel := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if s, ok := c.status[group]; ok {
			s.mu.Lock()
			delete(s.watches, watchID)
			s.mu.Unlock()
		}
	}
	return cancel
}

func (c *Cache) fillStatus(group string, request *source.Request, peerAddr string) *StatusInfo {

	info, ok := c.status[group]
	if !ok {
		info = &StatusInfo{
			watches: make(map[int64]*responseWatch),
			synced:  make(map[string]map[string]bool),
		}
		peerStatus := make(map[string]bool)
		peerStatus[peerAddr] = false
		info.synced[request.Collection] = peerStatus
		c.status[group] = info
	} else {
		collectionExists := false
		peerExists := false
		for collection, synced := range info.synced {
			if collection == request.Collection {
				collectionExists = true
				for addr := range synced {
					if addr == peerAddr {
						peerExists = true
						break
					}
				}
			}
			if collectionExists && peerExists {
				break
			}
		}
		if !collectionExists {
			// initiate the synced map
			peerStatus := make(map[string]bool)
			peerStatus[peerAddr] = false
			info.synced[request.Collection] = peerStatus
		}
		if !peerExists {
			info.synced[request.Collection][peerAddr] = false
		}
	}

	// update last responseWatch request time
	info.mu.Lock()
	info.lastWatchRequestTime = time.Now()
	info.mu.Unlock()

	return info
}

// SetSnapshot updates a snapshot for a group.
func (c *Cache) SetSnapshot(group string, snapshot Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// update the existing entry
	c.snapshots[group] = snapshot

	// trigger existing watches for which version changed
	if info, ok := c.status[group]; ok {
		info.mu.Lock()
		defer info.mu.Unlock()

		for id, watch := range info.watches {
			version := snapshot.Version(watch.request.Collection)
			if version != watch.request.VersionInfo {
				scope.Infof("SetSnapshot(): respond to watch %d for %v @ version %q",
					id, watch.request.Collection, version)

				response := &source.WatchResponse{
					Collection: watch.request.Collection,
					Version:    version,
					Resources:  snapshot.Resources(watch.request.Collection),
					Request:    watch.request,
				}
				watch.pushResponse(response)

				// discard the responseWatch
				delete(info.watches, id)

				scope.Debugf("SetSnapshot(): watch %d for %v @ version %q complete",
					id, watch.request.Collection, version)
			}
		}
	}
}

// ClearSnapshot clears snapshot for a group. This does not cancel any open
// watches already created (see ClearStatus).
func (c *Cache) ClearSnapshot(group string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.snapshots, group)
}

// ClearStatus clears status for a group. This has the effect of canceling
// any open watches opened against this group info.
func (c *Cache) ClearStatus(group string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if info, ok := c.status[group]; ok {
		info.mu.Lock()
		for _, watch := range info.watches {
			// response channel may be shared
			watch.pushResponse(nil)
		}
		info.mu.Unlock()
	}
	delete(c.status, group)
}

// Status returns informational status for a group.
func (c *Cache) Status(group string) *StatusInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if info, ok := c.status[group]; ok {
		return info
	}
	return nil
}

// GetGroups returns all groups of snapshots that the server layer is serving.
func (c *Cache) GetGroups() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	groups := make([]string, 0, len(c.snapshots))

	for t := range c.snapshots {
		groups = append(groups, t)
	}
	sort.Strings(groups)

	return groups
}

// GetSnapshotInfo return the snapshots information
func (c *Cache) GetSnapshotInfo(group string) []Info {

	// if the group is empty, then use the default one
	if group == "" {
		group = c.GetGroups()[0]
	}

	if snapshot, ok := c.snapshots[group]; ok {

		var snapshots []Info
		collections := snapshot.Collections()

		// sort the collections
		sort.Strings(collections)

		for _, collection := range collections {
			entrieNames := make([]string, 0, len(snapshot.Resources(collection)))
			for _, entry := range snapshot.Resources(collection) {
				entrieNames = append(entrieNames, entry.Metadata.Name)
			}
			// sort the mcp resource names
			sort.Strings(entrieNames)

			synced := make(map[string]bool)
			if statusInfo, found := c.status[group]; found {
				synced = statusInfo.synced[collection]
			}

			info := Info{
				Collection: collection,
				Version:    snapshot.Version(collection),
				Names:      entrieNames,
				Synced:     synced,
			}
			snapshots = append(snapshots, info)
		}
		return snapshots
	}
	return nil
}

// GetResource returns the mcp resource detailed information for the specified group
func (c *Cache) GetResource(group string, collection string, resourceName string) *sink.Object {

	c.mu.Lock()
	defer c.mu.Unlock()

	// if the group or collection is empty, return empty
	if group == "" || collection == "" {
		return nil
	}

	if snapshot, ok := c.snapshots[group]; ok {
		for _, resource := range snapshot.Resources(collection) {
			if resource.Metadata.Name == resourceName {
				var dynamicAny types.DynamicAny
				if err := types.UnmarshalAny(resource.Body, &dynamicAny); err == nil {
					return &sink.Object{
						TypeURL:  resource.Body.TypeUrl,
						Metadata: resource.Metadata,
						Body:     dynamicAny.Message,
					}
				}
			}
		}
	}
	return nil
}
