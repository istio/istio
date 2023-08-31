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

package autoregistration

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/maps"
)

// connection from a proxy to a control plane.
// This interface exists to avoid circular reference to xds.Connection as well
// as keeps the API surface scoped to what we need for auto-registration logic.
type connection interface {
	ID() string
	Proxy() *model.Proxy
	ConnectedAt() time.Time
	Stop()
}

// a single proxy may have multiple connections, so we track them here
// when a WorkloadGroup is deleted, we can force disconnects
// when OnDisconnect occurs, we only trigger cleanup when there are no more connections for that proxy
type adsConnections struct {
	sync.Mutex

	// keyed by proxy id, then connection id
	byProxy map[proxyKey]map[string]connection
}

func newAdsConnections() *adsConnections {
	return &adsConnections{byProxy: map[proxyKey]map[string]connection{}}
}

func (m *adsConnections) ConnectionsForGroup(wg types.NamespacedName) []connection {
	// collect the proxies that should be disconnected (don't remove them, OnDisconnect will)
	m.Lock()
	defer m.Unlock()
	var conns []connection
	for key, connections := range m.byProxy {
		if key.GroupName == wg.Name && key.Namespace == wg.Namespace {
			conns = append(conns, maps.Values(connections)...)
		}
	}
	return conns
}

func (m *adsConnections) Connect(conn connection) {
	m.Lock()
	defer m.Unlock()
	k := makeProxyKey(conn.Proxy())

	connections := m.byProxy[k]
	if connections == nil {
		connections = make(map[string]connection)
		m.byProxy[k] = connections
	}
	connections[conn.ID()] = conn
}

// Disconnect tracks disconnect events of ads clients.
// Returns false once there are no more connections for the given proxy.
func (m *adsConnections) Disconnect(conn connection) bool {
	m.Lock()
	defer m.Unlock()

	k := makeProxyKey(conn.Proxy())
	connections := m.byProxy[k]
	if connections == nil {
		return false
	}

	id := conn.ID()
	delete(connections, id)
	if len(connections) == 0 {
		delete(m.byProxy, k)
		return false
	}

	return true
}

// keys required to uniquely ID a single proxy
type proxyKey struct {
	Network   string
	IP        string
	GroupName string
	Namespace string
}

func makeProxyKey(proxy *model.Proxy) proxyKey {
	return proxyKey{
		Network:   string(proxy.Metadata.Network),
		IP:        proxy.IPAddresses[0],
		GroupName: proxy.Metadata.AutoRegisterGroup,
		Namespace: proxy.Metadata.Namespace,
	}
}
