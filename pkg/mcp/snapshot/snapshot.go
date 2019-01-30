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

package snapshot

import (
	"sync"
	"time"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/source"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// Snapshot provides an immutable view of versioned resources.
type Snapshot interface {
	Resources(collection string) []*mcp.Resource
	Version(collection string) string
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

// GroupIndexFn returns a stable group index for the given MCP node.
type GroupIndexFn func(node *mcp.SinkNode) string

// DefaultGroup is the default group when using the DefaultGroupIndex() function.
const DefaultGroup = "default"

// DefaultGroupIndex provides a default GroupIndexFn function that
// is usable for testing and simple deployments.
func DefaultGroupIndex(_ *mcp.SinkNode) string {
	return DefaultGroup
}

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

// StatusInfo records watch status information of a remote node.
type StatusInfo struct {
	mu                   sync.RWMutex
	node                 *mcp.SinkNode
	lastWatchRequestTime time.Time // informational
	watches              map[int64]*responseWatch
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
func (c *Cache) Watch(request *source.Request, pushResponse source.PushResponseFunc) source.CancelWatchFunc { // nolint: lll
	group := c.groupIndex(request.SinkNode)

	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.status[group]
	if !ok {
		info = &StatusInfo{
			node:    request.SinkNode,
			watches: make(map[int64]*responseWatch),
		}
		c.status[group] = info
	}

	// update last responseWatch request time
	info.mu.Lock()
	info.lastWatchRequestTime = time.Now()
	info.mu.Unlock()

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
		if info, ok := c.status[group]; ok {
			info.mu.Lock()
			delete(info.watches, watchID)
			info.mu.Unlock()
		}
	}
	return cancel
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
