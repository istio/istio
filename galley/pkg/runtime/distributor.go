//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package runtime

import (
	"sync"

	sn "istio.io/istio/pkg/mcp/snapshot"
)

// Distributor interface allows processor to distribute snapshots of configuration.
type Distributor interface {
	SetSnapshot(name string, snapshot sn.Snapshot)

	ClearSnapshot(name string)
}

// InMemoryDistributor is an in-memory distributor implementation.
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

	d.snapshots[name] = snapshot
}

// ClearSnapshot is an implementation of Distributor.ClearSnapshot
func (d *InMemoryDistributor) ClearSnapshot(name string) {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()

	delete(d.snapshots, name)
}
