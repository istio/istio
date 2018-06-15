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

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	mcp "istio.io/api/config/mcp/v1alpha1"
	"istio.io/fortio/log"
	"istio.io/istio/galley/pkg/mcp/server"
)

// Snapshot provides an immutable view of versioned resources.
type Snapshot interface {
	Resources(typ string) []*mcp.Envelope
	Version(typ string) string
}

// Cache is a snapshot-based cache that maintains a single versioned
// snapshot of responses per node. Cache consistently replies with the
// latest snapshot.
type Cache struct {
	mu         sync.RWMutex
	snapshots  map[string]Snapshot
	status     map[string]*statusInfo
	watchCount int64
}

// New creates a new cache of resource snapshots.
func New() *Cache {
	return &Cache{
		snapshots: make(map[string]Snapshot),
		status:    make(map[string]*statusInfo),
	}
}

var _ server.Watcher = &Cache{}

type responseWatch struct {
	request   *xdsapi.DiscoveryRequest // original request
	responseC chan<- *server.WatchResponse
}

type statusInfo struct {
	mu                   sync.Mutex
	node                 *core.Node
	lastWatchRequestTime time.Time
	watches              map[int64]*responseWatch
}

// Watch returns a watch for an MCP request.
func (c *Cache) Watch(request *xdsapi.DiscoveryRequest, responseC chan<- *server.WatchResponse) (*server.WatchResponse, server.CancelWatchFunc) { // nolint: lll
	nodeID := request.Node.GetId()

	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.status[nodeID]
	if !ok {
		info = &statusInfo{
			node:    request.Node,
			watches: make(map[int64]*responseWatch),
		}
		c.status[nodeID] = info
	}

	// update last responseWatch request time
	info.mu.Lock()
	info.lastWatchRequestTime = time.Now()
	info.mu.Unlock()

	// return an immediate response if a snapshot is available and the
	// requested version doesn't match.
	if snapshot, ok := c.snapshots[nodeID]; ok {
		if version := snapshot.Version(request.TypeUrl); version != request.VersionInfo {
			response := &server.WatchResponse{
				TypeURL:   request.TypeUrl,
				Version:   version,
				Resources: snapshot.Resources(request.TypeUrl),
			}
			return response, nil
		}
	}

	// Otherwise, open a watch if no snapshot was available or the requested version1 is up-to-date.
	c.watchCount++
	watchID := c.watchCount

	log.Infof("CreateWatch(): create watch %d for %s from nodeID %q, version1 %q",
		watchID, request.TypeUrl, nodeID, request.VersionInfo)

	info.mu.Lock()
	info.watches[watchID] = &responseWatch{request: request, responseC: responseC}
	info.mu.Unlock()

	cancel := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if info, ok := c.status[nodeID]; ok {
			info.mu.Lock()
			delete(info.watches, watchID)
			info.mu.Unlock()
		}
	}
	return nil, cancel
}

// SetSnapshot updates a snapshot for a node.
func (c *Cache) SetSnapshot(node string, snapshot Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// update the existing entry
	c.snapshots[node] = snapshot

	// trigger existing watches for which version1 changed
	if info, ok := c.status[node]; ok {
		info.mu.Lock()
		for id, watch := range info.watches {
			version := snapshot.Version(watch.request.TypeUrl)
			if version != watch.request.VersionInfo {
				log.Infof("SetSnapshot(): respond to watch %d with new version1 %q", id, version)

				response := &server.WatchResponse{
					TypeURL:   watch.request.TypeUrl,
					Version:   version,
					Resources: snapshot.Resources(watch.request.TypeUrl),
				}
				watch.responseC <- response

				// discard the responseWatch
				delete(info.watches, id)
			}
		}
		info.mu.Unlock()
	}
}

// ClearSnapshot clears snapshot and info for a node.
func (c *Cache) ClearSnapshot(node string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.snapshots, node)
	delete(c.status, node)
}
