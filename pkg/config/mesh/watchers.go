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
	"istio.io/api/mesh/v1alpha1"
)

// NetworksWatcher watches changes to the mesh networks config.
type NetworksWatcher interface {
	Networks() *v1alpha1.MeshNetworks

	// AddNetworksHandler registers a callback handler for changes to the networks config.
	AddNetworksHandler(func()) *WatcherHandlerRegistration

	// DeleteNetworksHandler unregisters a callback handler when remote cluster is removed.
	DeleteNetworksHandler(registration *WatcherHandlerRegistration)
}

// Holder of a mesh configuration.
type Holder interface {
	Mesh() *v1alpha1.MeshConfig
}

// Watcher is a Holder whose mesh config can be updated asynchronously.
type Watcher interface {
	Holder

	// AddMeshHandler registers a callback handler for changes to the mesh config.
	AddMeshHandler(h func()) *WatcherHandlerRegistration

	// DeleteMeshHandler unregisters a callback handler when remote cluster is removed.
	DeleteMeshHandler(registration *WatcherHandlerRegistration)
}

// WatcherHandlerRegistration will be returned to caller to remove the handler later.
type WatcherHandlerRegistration struct {
	remove func()
}

func NewWatcherHandlerRegistration(f func()) *WatcherHandlerRegistration {
	return &WatcherHandlerRegistration{remove: f}
}

func (r *WatcherHandlerRegistration) Remove() {
	r.remove()
}

// RestrictedConfigWatcher provides limited access to mesh configuration.
// It exposes only trust domain and service scope, suitable for use in remote clusters
// or components that should not have full mesh config access.
type RestrictedConfigWatcher interface {
	TrustDomain() string
	ServiceScopeConfigs() []*v1alpha1.MeshConfig_ServiceScopeConfigs
}

// NewRestrictedConfigWatcher wraps a Holder to expose only trust domain and service scope.
func NewRestrictedConfigWatcher(holder Holder) RestrictedConfigWatcher {
	return restrictedConfigAdapter{holder}
}

// restrictedConfigAdapter wraps a Holder to provide RestrictedConfigWatcher interface.
type restrictedConfigAdapter struct {
	Holder
}

func (r restrictedConfigAdapter) TrustDomain() string {
	return r.Mesh().GetTrustDomain()
}

func (r restrictedConfigAdapter) ServiceScopeConfigs() []*v1alpha1.MeshConfig_ServiceScopeConfigs {
	return r.Mesh().GetServiceScopeConfigs()
}
