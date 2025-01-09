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
	uatomic "go.uber.org/atomic"
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

func ConfigAdapter(configuration krt.Singleton[MeshConfigResource]) WatcherCollection {
	return adapter{configuration}
}

type adapter struct {
	krt.Singleton[MeshConfigResource]
}

func (a adapter) Mesh() *meshconfig.MeshConfig {
	v := a.Singleton.Get()
	return v.MeshConfig
}

func (a adapter) AddMeshHandler(h func()) *mesh.WatcherHandlerRegistration {
	active := uatomic.NewBool(true)
	reg := mesh.NewWatcherHandlerRegistration(func() {
		active.Store(false)
	})
	// Do not run initial state to match existing semantics
	a.Singleton.AsCollection().RegisterBatch(func(o []krt.Event[MeshConfigResource], initialSync bool) {
		if active.Load() {
			h()
		}
	}, false)
	return reg
}

func (a adapter) DeleteMeshHandler(registration *mesh.WatcherHandlerRegistration) {
	registration.Remove()
}

var _ mesh.Watcher = adapter{}

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

func NetworksAdapter(configuration krt.Singleton[MeshNetworksResource]) mesh.NetworksWatcher {
	return networksAdapter{configuration}
}
