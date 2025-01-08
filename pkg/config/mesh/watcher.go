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

package mesh

import (
	uatomic "go.uber.org/atomic"
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/protomarshal"
)

// Holder of a mesh configuration.
type Holder interface {
	Mesh() *meshconfig.MeshConfig
}

// Watcher is a Holder whose mesh config can be updated asynchronously.
type Watcher interface {
	Holder

	// AddMeshHandler registers a callback handler for changes to the mesh config.
	AddMeshHandler(h func()) *WatcherHandlerRegistration

	// DeleteMeshHandler unregisters a callback handler when remote cluster is removed.
	DeleteMeshHandler(registration *WatcherHandlerRegistration)
}

// NewFixedWatcher creates a new Watcher that always returns the given mesh config. It will never
// fire any events, since the config never changes.
func NewFixedWatcher(mesh *meshconfig.MeshConfig) Watcher {
	return adapter{krt.NewStatic(&MeshConfigResource{mesh}, true)}
}

type adapter struct {
	krt.Singleton[MeshConfigResource]
}

func (a adapter) Mesh() *meshconfig.MeshConfig {
	v := a.Singleton.Get()
	return v.MeshConfig
}

func (a adapter) AddMeshHandler(h func()) *WatcherHandlerRegistration {
	active := uatomic.NewBool(true)
	reg := &WatcherHandlerRegistration{
		remove: func() {
			active.Store(false)
		},
	}
	// Do not run initial state to match existing semantics
	a.Singleton.AsCollection().RegisterBatch(func(o []krt.Event[MeshConfigResource], initialSync bool) {
		if active.Load() {
			h()
		}
	}, false)
	return reg
}

func (a adapter) DeleteMeshHandler(registration *WatcherHandlerRegistration) {
	registration.remove()
}

var _ Watcher = adapter{}

func PrettyFormatOfMeshConfig(meshConfig *meshconfig.MeshConfig) string {
	meshConfigDump, _ := protomarshal.ToJSONWithIndent(meshConfig, "    ")
	return meshConfigDump
}

type MeshConfigResource struct {
	*meshconfig.MeshConfig
}

func (m MeshConfigResource) ResourceName() string { return "MeshConfigResource" }

func (m MeshConfigResource) Equals(other MeshConfigResource) bool {
	return proto.Equal(m.MeshConfig, other.MeshConfig)
}

type MeshNetworksResource struct {
	*meshconfig.MeshNetworks
}

func (m MeshNetworksResource) ResourceName() string { return "MeshNetworksResource" }

func (m MeshNetworksResource) Equals(other MeshNetworksResource) bool {
	return proto.Equal(m.MeshNetworks, other.MeshNetworks)
}

func ConfigAdapter(configuration krt.Singleton[MeshConfigResource]) Watcher {
	return adapter{configuration}
}

func NetworksAdapter(configuration krt.Singleton[MeshNetworksResource]) NetworksWatcher {
	return networksAdapter{configuration}
}
