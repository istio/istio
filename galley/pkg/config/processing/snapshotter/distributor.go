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

package snapshotter

import (
	"sync"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/mcp/snapshot"
)

// Distributor interface abstracts the snapshotImpl distribution mechanism. Typically, this is implemented by the
// MCP layer.
type Distributor interface {
	Distribute(name string, s *Snapshot)
}

// MCPDistributor distributes a snapshot to the MCP layer.
type MCPDistributor struct {
	mcpCache *snapshot.Cache
}

// Distribute is an implementation of SetSnapshot
func (d *MCPDistributor) Distribute(name string, s *Snapshot) {
	d.mcpCache.SetSnapshot(name, s)
}

// NewMCPDistributor returns a new instance of MCPDistributor
func NewMCPDistributor(c *snapshot.Cache) *MCPDistributor {
	return &MCPDistributor{mcpCache: c}
}

// InMemoryDistributor is an in-memory Distributor implementation.
type InMemoryDistributor struct {
	snapshotsLock sync.Mutex
	snapshots     map[string]snapshot.Snapshot
}

var _ Distributor = &InMemoryDistributor{}

// NewInMemoryDistributor returns a new instance of InMemoryDistributor
func NewInMemoryDistributor() *InMemoryDistributor {
	return &InMemoryDistributor{
		snapshots: make(map[string]snapshot.Snapshot),
	}
}

// Distribute is an implementation of Distributor.Distribute
func (d *InMemoryDistributor) Distribute(name string, s *Snapshot) {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()

	scope.Processing.Debugf("InmemoryDistributor.Distribute: %s: %v", name, s)
	d.snapshots[name] = s
}

// GetSnapshot get the snapshotImpl of the specified name
func (d *InMemoryDistributor) GetSnapshot(name string) snapshot.Snapshot {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()
	if s, ok := d.snapshots[name]; ok {
		return s
	}
	return nil
}

// NumSnapshots returns the current number of snapshots.
func (d *InMemoryDistributor) NumSnapshots() int {
	return len(d.snapshots)
}
