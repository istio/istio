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

package snapshotter

import (
	"sync"

	sn "istio.io/istio/pkg/mcp/snapshot"
)

// Distributor interface abstracts the snapshot distribution mechanism. Typically, this is implemented by the MCP layer.
type Distributor interface {
	SetSnapshot(name string, snapshot sn.Snapshot)
}

// InMemoryDistributor is an in-memory Distributor implementation.
type InMemoryDistributor struct {
	snapshotsLock sync.Mutex
	snapshots     map[string]sn.Snapshot
}

var _ Distributor = &InMemoryDistributor{}

// NewInMemoryDistributor returns a new instance of InMemoryDistributor
func NewInMemoryDistributor() *InMemoryDistributor {
	return &InMemoryDistributor{
		snapshots: make(map[string]sn.Snapshot),
	}
}

// SetSnapshot is an implementation of Distributor.SetSnapshot
func (d *InMemoryDistributor) SetSnapshot(name string, snapshot sn.Snapshot) {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()

	scope.Infof("InmemoryDistributor.SetSnapshot: %s: %v", name, snapshot)
	d.snapshots[name] = snapshot
}

// GetSnapshot get the snapshot of the specified name
func (d *InMemoryDistributor) GetSnapshot(name string) sn.Snapshot {
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
