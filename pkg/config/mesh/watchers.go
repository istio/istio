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

// NewRestrictedConfigWatcher wraps a primary Holder to expose only trust domain and service scope.
// An optional fallback Holder can be provided; if the primary returns nil or an empty trust domain,
// the fallback is consulted. This is used for remote clusters where the remote meshconfig may not
// be readable (e.g. during upgrades before RBAC is applied), falling back to the local cluster's config.
func NewRestrictedConfigWatcher(primary Holder, fallback ...Holder) RestrictedConfigWatcher {
	var fb Holder
	if len(fallback) > 0 {
		fb = fallback[0]
	}
	return restrictedConfigAdapter{primary: primary, fallback: fb}
}

// restrictedConfigAdapter wraps a Holder to provide RestrictedConfigWatcher interface.
type restrictedConfigAdapter struct {
	primary  Holder
	fallback Holder
}

func (r restrictedConfigAdapter) TrustDomain() string {
	if m := r.primary.Mesh(); m != nil {
		if td := m.GetTrustDomain(); td != "" {
			return td
		}
	}
	if r.fallback != nil {
		return r.fallback.Mesh().GetTrustDomain()
	}
	return ""
}

func (r restrictedConfigAdapter) ServiceScopeConfigs() []*v1alpha1.MeshConfig_ServiceScopeConfigs {
	if m := r.primary.Mesh(); m != nil {
		return m.GetServiceScopeConfigs()
	}
	if r.fallback != nil {
		return r.fallback.Mesh().GetServiceScopeConfigs()
	}
	return nil
}
