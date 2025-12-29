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

type networksAdapter struct {
	krt.Singleton[MeshNetworksResource]
}

func (n networksAdapter) Networks() *meshconfig.MeshNetworks {
	v := n.Singleton.Get()
	return v.MeshNetworks
}

// AddNetworksHandler registers a callback handler for changes to the networks config.
func (n networksAdapter) AddNetworksHandler(h func()) *mesh.WatcherHandlerRegistration {
	colReg := n.Singleton.AsCollection().RegisterBatch(func(o []krt.Event[MeshNetworksResource]) {
		h()
	}, false)

	reg := mesh.NewWatcherHandlerRegistration(func() {
		colReg.UnregisterHandler()
	})
	return reg
}

func (n networksAdapter) DeleteNetworksHandler(registration *mesh.WatcherHandlerRegistration) {
	registration.Remove()
}

var _ mesh.NetworksWatcher = networksAdapter{}
