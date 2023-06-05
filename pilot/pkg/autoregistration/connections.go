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
	"istio.io/istio/pkg/maps"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
)

// Connection from a proxy to a control plane.
// This interface exists to avoid circular reference to xds.Connection as well
// as keeps the API surface scoped to what we need for auto-registration logic.
type Connection interface {
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
	byProxy map[proxyKey]map[string]Connection
}

func newAdsConnections() *adsConnections {
	return &adsConnections{byProxy: map[proxyKey]map[string]Connection{}}
}

func (m *adsConnections) ForceDisconnectGroup(wg types.NamespacedName) {
	// collect the proxies that should be disconnected (don't remove them, OnDisconnect will)
	proxies := 0
	m.Lock()
	var conns []Connection
	for key, connections := range m.byProxy {
		if key.GroupName == wg.Name && key.Namespace == wg.Namespace {
			proxies++
			conns = append(conns, maps.Values(connections)...)
		}
	}
	m.Unlock()

	stopped := 0
	for _, connection := range conns {
		// should never happen..
		if !connection.Proxy().WorkloadEntryAutoCreated {
			log.Errorf("auto-registration controller incorrectly tracking connection for %s", connection.Proxy().ID)
			continue
		}
		stopped++
		connection.Stop()
	}
	log.Infof("WorkloadGroup deletion for %s/%s: force closed %d connections for %d proxies",
		wg.Namespace, wg.Name, stopped, proxies)
}

func (m *adsConnections) Connect(conn Connection) {
	m.Lock()
	defer m.Unlock()
	k := makeProxyKey(conn.Proxy())

	connections := m.byProxy[k]
	if connections == nil {
		connections = make(map[string]Connection)
		m.byProxy[k] = connections
	}
	connections[conn.ID()] = conn
}

// Disconnect tracks disconnect events of ads clients.
// Returns false once there are no more connections for the given proxy.
func (m *adsConnections) Disconnect(conn Connection) bool {
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
