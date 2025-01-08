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
	"sync"

	uatomic "go.uber.org/atomic"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/slices"
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

var _ NetworksWatcher = &internalNetworkWatcher{}

type internalNetworkWatcher struct {
	mutex    sync.RWMutex
	handlers []*WatcherHandlerRegistration
	networks *meshconfig.MeshNetworks
}

// NewFixedNetworksWatcher creates a new NetworksWatcher that always returns the given config.
// It will never fire any events, since the config never changes.
func NewFixedNetworksWatcher(networks *meshconfig.MeshNetworks) NetworksWatcher {
	return &internalNetworkWatcher{
		networks: networks,
	}
}

type networksAdapter struct {
	files.Singleton[MeshNetworksResource]
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

// NewNetworksWatcher creates a new watcher for changes to the given networks config file.
func NewNetworksWatcher(fileWatcher filewatcher.FileWatcher, filename string) (NetworksWatcher, error) {
	col, err := files.NewSingleton[MeshNetworksResource](fileWatcher, filename, make(chan struct{}), readMeshNetworksResource, krt.WithName("MeshNetworks"))
	if err != nil {
		return nil, err
	}
	return networksAdapter{col}, nil
}

// Networks returns the latest network configuration for the mesh.
func (w *internalNetworkWatcher) Networks() *meshconfig.MeshNetworks {
	if w == nil {
		return nil
	}
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.networks
}

// AddNetworksHandler registers a callback handler for changes to the mesh network config.
func (w *internalNetworkWatcher) AddNetworksHandler(h func()) *WatcherHandlerRegistration {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	handler := &WatcherHandlerRegistration{
		// handler: h,
	}
	w.handlers = append(w.handlers, handler)
	return handler
}

// DeleteNetworksHandler deregister a callback handler for changes to the mesh network config.
func (w *internalNetworkWatcher) DeleteNetworksHandler(registration *WatcherHandlerRegistration) {
	if registration == nil {
		return
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if len(w.handlers) == 0 {
		return
	}

	w.handlers = slices.FilterInPlace(w.handlers, func(handler *WatcherHandlerRegistration) bool {
		return handler != registration
	})
}
