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
	"fmt"
	"reflect"
	"sync"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

// NetworksHolder is a holder of a mesh networks configuration.
type NetworksHolder interface {
	SetNetworks(*meshconfig.MeshNetworks)
	Networks() *meshconfig.MeshNetworks
	PrevNetworks() *meshconfig.MeshNetworks
}

// NetworksWatcher watches changes to the mesh networks config.
type NetworksWatcher interface {
	NetworksHolder

	AddNetworksHandler(func())
}

var _ NetworksWatcher = &internalNetworkWatcher{}

type internalNetworkWatcher struct {
	mutex        sync.RWMutex
	handlers     []func()
	networks     *meshconfig.MeshNetworks
	prevNetworks *meshconfig.MeshNetworks
}

// NewFixedNetworksWatcher creates a new NetworksWatcher that always returns the given config.
// It will never fire any events, since the config never changes.
func NewFixedNetworksWatcher(networks *meshconfig.MeshNetworks) NetworksWatcher {
	return &internalNetworkWatcher{
		networks: networks,
	}
}

// NewNetworksWatcher creates a new watcher for changes to the given networks config file.
func NewNetworksWatcher(fileWatcher filewatcher.FileWatcher, filename string) (NetworksWatcher, error) {
	meshNetworks, err := ReadMeshNetworks(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read mesh networks configuration from %q: %v", filename, err)
	}

	networksdump, _ := protomarshal.ToJSONWithIndent(meshNetworks, "   ")
	log.Infof("mesh networks configuration: %s", networksdump)

	w := &internalNetworkWatcher{
		networks: meshNetworks,
	}

	// Watch the networks config file for changes and reload if it got modified
	addFileWatcher(fileWatcher, filename, func() {
		// Reload the config file
		meshNetworks, err := ReadMeshNetworks(filename)
		if err != nil {
			log.Warnf("failed to read mesh networks configuration from %q: %v", filename, err)
			return
		}
		w.SetNetworks(meshNetworks)
	})
	return w, nil
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

// PrevNetworks returns the previous network configuration for the mesh.
func (w *internalNetworkWatcher) PrevNetworks() *meshconfig.MeshNetworks {
	if w == nil {
		return nil
	}
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.prevNetworks
}

// SetNetworks will use the given value for mesh networks and notify all handlers of the change
func (w *internalNetworkWatcher) SetNetworks(meshNetworks *meshconfig.MeshNetworks) {
	var handlers []func()

	w.mutex.Lock()
	if !reflect.DeepEqual(meshNetworks, w.networks) {
		networksdump, _ := protomarshal.ToJSONWithIndent(meshNetworks, "    ")
		log.Infof("mesh networks configuration updated to: %s", networksdump)

		// Store the new config.
		w.prevNetworks = w.networks
		w.networks = meshNetworks
		handlers = append([]func(){}, w.handlers...)
	}
	w.mutex.Unlock()

	// Notify the handlers of the change.
	for _, h := range handlers {
		h()
	}
}

// AddNetworksHandler registers a callback handler for changes to the mesh network config.
func (w *internalNetworkWatcher) AddNetworksHandler(h func()) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// hack: prepend handlers; the last to be added will be run first and block other handlers
	w.handlers = append([]func(){h}, w.handlers...)
}
