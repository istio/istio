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

package multicluster

import (
	"sync"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/maps"
)

type ComponentConstraint interface {
	Close()
	HasSynced() bool
}

type Component[T ComponentConstraint] struct {
	mu          sync.RWMutex
	constructor func(cluster *Cluster) T
	clusters    map[cluster.ID]T
}

func (m *Component[T]) ForCluster(clusterID cluster.ID) *T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, f := m.clusters[clusterID]
	if !f {
		return nil
	}
	return &t
}

func (m *Component[T]) All() []T {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Values(m.clusters)
}

func (m *Component[T]) clusterAdded(cluster *Cluster) ComponentConstraint {
	comp := m.constructor(cluster)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[cluster.ID] = comp
	return comp
}

func (m *Component[T]) clusterUpdated(cluster *Cluster) {
	// Build outside of the lock, in case its slow
	comp := m.constructor(cluster)
	old, f := m.clusters[cluster.ID]
	m.mu.Lock()
	m.clusters[cluster.ID] = comp
	m.mu.Unlock()
	// Close outside of the lock, in case its slow
	if f {
		old.Close()
	}
}

func (m *Component[T]) clusterDeleted(cluster cluster.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// If there is an old one, close it
	if old, f := m.clusters[cluster]; f {
		old.Close()
	}
	delete(m.clusters, cluster)
}

func (m *Component[T]) HasSynced() bool {
	for _, c := range m.All() {
		if !c.HasSynced() {
			return false
		}
	}
	return true
}
