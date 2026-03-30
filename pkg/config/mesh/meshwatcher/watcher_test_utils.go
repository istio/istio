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

package meshwatcher

import (
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/krt"
)

var _ mesh.RestrictedConfigWatcher = &TestWatcher{}

// TestWatcher provides an interface that takes a static MeshConfig which can be updated explicitly.
// It is intended for tests
type TestWatcher struct {
	adapter
	col krt.StaticSingleton[MeshConfigResource]
}

func (w TestWatcher) Set(n *meshconfig.MeshConfig) {
	w.col.Set(&MeshConfigResource{n})
}

func (w TestWatcher) TrustDomain() string {
	return w.Mesh().GetTrustDomain()
}

func (w TestWatcher) ServiceScopeConfigs() []*meshconfig.MeshConfig_ServiceScopeConfigs {
	return w.Mesh().GetServiceScopeConfigs()
}

// NewTestWatcher creates a new Watcher that always returns the given mesh config.
func NewTestWatcher(m *meshconfig.MeshConfig) TestWatcher {
	if m == nil {
		m = mesh.DefaultMeshConfig()
	}
	col := krt.NewStatic(&MeshConfigResource{m}, true, krt.WithName("MeshConfig"), krt.WithDebugging(krt.GlobalDebugHandler))
	a := adapter{col}
	return TestWatcher{
		adapter: a,
		col:     col,
	}
}

// TestNetworksWatcher provides an interface that takes a static MeshNetworks which can be updated explicitly.
// It is intended for tests
type TestNetworksWatcher struct {
	networksAdapter
	col krt.StaticSingleton[MeshNetworksResource]
}

func (w TestNetworksWatcher) SetNetworks(n *meshconfig.MeshNetworks) {
	w.col.Set(&MeshNetworksResource{n})
}

// NewFixedNetworksWatcher creates a new NetworksWatcher that always returns the given config.
func NewFixedNetworksWatcher(networks *meshconfig.MeshNetworks) TestNetworksWatcher {
	col := krt.NewStatic(&MeshNetworksResource{networks}, true)
	a := networksAdapter{col}
	return TestNetworksWatcher{
		networksAdapter: a,
		col:             col,
	}
}
