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
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/protomarshal"
)

// WatcherCollection is an interface to describe an object that implements both the legacy mesh.Watcher interface and the
// new krt interface.
type WatcherCollection interface {
	mesh.Watcher
	krt.Singleton[MeshConfigResource]
}

// ConfigAdapter wraps a MeshConfig collection into a mesh.Watcher interface.
func ConfigAdapter(configuration krt.Singleton[MeshConfigResource]) WatcherCollection {
	return adapter{configuration}
}

// adapter is a helper to expose the krt collection using the older mesh.Watcher interface
type adapter struct {
	krt.Singleton[MeshConfigResource]
}

var _ mesh.Watcher = adapter{}

// Mesh returns the current MeshConfig
func (a adapter) Mesh() *meshconfig.MeshConfig {
	// Just get the value; we know there is always one set due to the way the collection is setup.
	v := a.Singleton.Get()
	return v.MeshConfig
}

// AddMeshHandler registers a callback that will be called anytime the MeshConfig changes.
// Usually a handler would then call .Mesh() to get the new state.
// The returned WatcherHandlerRegistration can be used to un-register the handler at a later time.
func (a adapter) AddMeshHandler(h func()) *mesh.WatcherHandlerRegistration {
	// Do not run initial state to match existing semantics
	colReg := a.Singleton.AsCollection().RegisterBatch(func(o []krt.Event[MeshConfigResource]) {
		h()
	}, false)
	reg := mesh.NewWatcherHandlerRegistration(func() {
		colReg.UnregisterHandler()
	})
	return reg
}

// DeleteMeshHandler removes a previously registered handler.
func (a adapter) DeleteMeshHandler(registration *mesh.WatcherHandlerRegistration) {
	registration.Remove()
}

// MeshConfigResource holds the current MeshConfig state
type MeshConfigResource struct {
	*meshconfig.MeshConfig
}

func (m MeshConfigResource) ResourceName() string { return "MeshConfigResource" }

func (m MeshConfigResource) Equals(other MeshConfigResource) bool {
	return proto.Equal(m.MeshConfig, other.MeshConfig)
}

// MeshNetworksResource holds the current MeshNetworks state
type MeshNetworksResource struct {
	*meshconfig.MeshNetworks
}

func (m MeshNetworksResource) ResourceName() string { return "MeshNetworksResource" }

func (m MeshNetworksResource) Equals(other MeshNetworksResource) bool {
	return proto.Equal(m.MeshNetworks, other.MeshNetworks)
}

// NetworksAdapter wraps a MeshNetworks collection into a mesh.NetworksWatcher interface.
func NetworksAdapter(configuration krt.Singleton[MeshNetworksResource]) mesh.NetworksWatcher {
	return networksAdapter{configuration}
}

func PrettyFormatOfMeshConfig(meshConfig *meshconfig.MeshConfig) string {
	meshConfigDump, _ := protomarshal.ToYAML(meshConfig)
	return meshConfigDump
}

func PrettyFormatOfMeshNetworks(meshNetworks *meshconfig.MeshNetworks) string {
	meshNetworksDump, _ := protomarshal.ToYAML(meshNetworks)
	return meshNetworksDump
}
