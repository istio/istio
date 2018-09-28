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
	"istio.io/istio/pkg/mcp/server"
)

var scope = log.RegisterScope("snapshot", "mcp snapshot", 0)

// Snapshot provides an immutable view of versioned envelopes.
type Snapshot interface {
	Resources(typ string) []*mcp.Envelope
	Version(typ string) string
}

// Cache is a snapshot-based cache that maintains a single versioned
// snapshot of responses per group of clients. Cache consistently replies with the
// latest snapshot.
type Cache struct {
	mu         sync.RWMutex
	snapshots  map[string]Snapshot
	status     map[string]*StatusInfo
	watchCount int64

	groupIndex GroupIndex
}

// GroupIndex returns a stable group index for the given MCP client.
type GroupIndex func(client *mcp.Client) string

// DefaultGroup is the default group when using the DefaultGroupIndex() function.
const DefaultGroup = "default"

// DefaultGroupIndex provides a default GroupIndex function that
// is usable for testing and simple deployments.
func DefaultGroupIndex(_ *mcp.Client) string {
	return DefaultGroup
}

// New creates a new cache of resource snapshots.
func New(groupIndex GroupIndex) *Cache {
	return &Cache{
		snapshots:  make(map[string]Snapshot),
		status:     make(map[string]*StatusInfo),
		groupIndex: groupIndex,
	}
}

var _ server.Watcher = &Cache{}

type responseWatch struct {
	request   *mcp.MeshConfigRequest // original request
	responseC chan<- *server.WatchResponse
}

// StatusInfo records watch status information of a remote client.
type StatusInfo struct {
	mu                   sync.RWMutex
	client               *mcp.Client
	lastWatchRequestTime time.Time // informational
	watches              map[int64]*responseWatch
	totalWatches         int
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
func (c *Cache) Watch(request *mcp.MeshConfigRequest, responseC chan<- *server.WatchResponse) (*server.WatchResponse, server.CancelWatchFunc) { // nolint: lll
	group := c.groupIndex(request.Client)

	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.status[group]
	if !ok {
		info = &StatusInfo{
			client:  request.Client,
			watches: make(map[int64]*responseWatch),
		}
		c.status[group] = info
	}

	// update last responseWatch request time
	info.mu.Lock()
	info.lastWatchRequestTime = time.Now()
	info.mu.Unlock()

	// return an immediate response if a snapshot is available and the
	// requested version doesn't match.
	if snapshot, ok := c.snapshots[group]; ok {
		version := snapshot.Version(request.TypeUrl)
		scope.Debugf("Found snapshot for group: %q, with version: %q", group, version)
		if version != request.VersionInfo {
			scope.Debugf("Responding to group %q with snapshot:\n%v\n", group, snapshot)
			response := &server.WatchResponse{
				TypeURL:   request.TypeUrl,
				Version:   version,
				Envelopes: snapshot.Resources(request.TypeUrl),
			}
			return response, nil
		}
	}

	// Otherwise, open a watch if no snapshot was available or the requested version is up-to-date.
	c.watchCount++
	watchID := c.watchCount

	log.Infof("Watch(): created watch %d for %s from group %q, version %q",
		watchID, request.TypeUrl, group, request.VersionInfo)

	info.mu.Lock()
	info.watches[watchID] = &responseWatch{request: request, responseC: responseC}
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
	return nil, cancel
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
		for id, watch := range info.watches {
			version := snapshot.Version(watch.request.TypeUrl)
			if version != watch.request.VersionInfo {
				log.Infof("SetSnapshot(): respond to watch %d with new version %q", id, version)

				response := &server.WatchResponse{
					TypeURL:   watch.request.TypeUrl,
					Version:   version,
					Envelopes: snapshot.Resources(watch.request.TypeUrl),
				}
				watch.responseC <- response

				// discard the responseWatch
				delete(info.watches, id)

				log.Debugf("SetSnapshot(): watch %d with version %q complete", id, version)
			}
		}
		info.mu.Unlock()
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
			watch.responseC <- nil
		}
		info.mu.Unlock()
	}
	delete(c.status, group)
}

// Status returns informational status for a group.
func (c *Cache) Status(group string) *StatusInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, ok := c.status[group]; ok {
		return info
	}
	return nil
}
