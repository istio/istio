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
