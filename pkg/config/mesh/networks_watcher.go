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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
)

// NetworksHolder is a holder of a mesh networks configuration.
type NetworksHolder interface {
	Networks() *meshconfig.MeshNetworks
}

// WatcherHandlerRegistration will be returned to caller to remove the handler later.
type WatcherHandlerRegistration struct {
	remove func()
}

// NetworksWatcher watches changes to the mesh networks config.
type NetworksWatcher interface {
	NetworksHolder

	// AddNetworksHandler registers a callback handler for changes to the networks config.
	AddNetworksHandler(func()) *WatcherHandlerRegistration

	// DeleteNetworksHandler unregisters a callback handler when remote cluster is removed.
	DeleteNetworksHandler(registration *WatcherHandlerRegistration)
}

// NewFixedNetworksWatcher creates a new NetworksWatcher that always returns the given config.
// It will never fire any events, since the config never changes.
func NewFixedNetworksWatcher(networks *meshconfig.MeshNetworks) NetworksWatcher {
	return networksAdapter{krt.NewStatic(&MeshNetworksResource{networks}, true)}
}

type networksAdapter struct {
	krt.Singleton[MeshNetworksResource]
}

func (n networksAdapter) Networks() *meshconfig.MeshNetworks {
	v := n.Singleton.Get()
	return v.MeshNetworks
}

func (n networksAdapter) AddNetworksHandler(h func()) *WatcherHandlerRegistration {
	active := uatomic.NewBool(true)
	reg := &WatcherHandlerRegistration{
		remove: func() {
			active.Store(false)
		},
	}
	// Do not run initial state to match existing semantics
	n.Singleton.AsCollection().RegisterBatch(func(o []krt.Event[MeshNetworksResource], initialSync bool) {
		if active.Load() {
			h()
		}
	}, false)
	return reg
}

func (n networksAdapter) DeleteNetworksHandler(registration *WatcherHandlerRegistration) {
	registration.remove()
}

var _ NetworksWatcher = networksAdapter{}
