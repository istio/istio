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

package kclient

import (
	klabels "k8s.io/apimachinery/pkg/labels"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube/controllers"
)

type Untyped = Informer[controllers.Object]

// Reader wraps a Kubernetes client providing cached read access.
// This is based on informers, so most of the same caveats to informers apply here.
type Reader[T controllers.Object] interface {
	// Get looks up an object by name and namespace. If it does not exist, nil is returned
	Get(name, namespace string) T
	// List looks up an object by namespace and labels.
	// Use metav1.NamespaceAll and klabels.Everything() to select everything.
	List(namespace string, selector klabels.Selector) []T
}

type Informer[T controllers.Object] interface {
	Reader[T]
	// ListUnfiltered is like List but ignores any *client side* filters previously configured.
	ListUnfiltered(namespace string, selector klabels.Selector) []T
	// AddEventHandler inserts a handler. The handler will be called for all Create/Update/Removals.
	// When ShutdownHandlers is called, the handler is removed.
	AddEventHandler(h cache.ResourceEventHandler) cache.ResourceEventHandlerRegistration
	// HasSynced returns true when the informer is initially populated and that all handlers added
	// via AddEventHandler have been called with the initial state.
	// note: this differs from a standard informer HasSynced, which does not check handlers have been called.
	HasSynced() bool
	// HasSyncedIgnoringHandlers returns true when the underlying informer has synced.
	// Warning: this ignores whether handlers are ready! HasSynced, which takes handlers into account, is recommended.
	// When using this, the ResourceEventHandlerRegistration from AddEventHandler can be used to check individual handlers
	HasSyncedIgnoringHandlers() bool
	// ShutdownHandlers terminates all handlers added by AddEventHandler.
	// Warning: this only applies to handlers called via AddEventHandler; any handlers directly added
	// to the underlying informer are not touched
	ShutdownHandlers()
	// ShutdownHandler shuts down a single handler added by AddEventHandler.
	// ShutdownHandlers can also be used to shutdown everything.
	ShutdownHandler(registration cache.ResourceEventHandlerRegistration)
	// Start starts just this informer. Typically, this is not used. Instead, the `kube.Client.Run()` is
	// used to start all informers at once.
	// However, in some cases we need to run individual informers directly.
	// This function should only be called once. It does not wait for the informer to become ready nor does it block,
	// so it should generally not be called in a goroutine.
	Start(stop <-chan struct{})
	// Index creates an index with a name. The extract function takes an object, and returns all keys to that object.
	// Later, all objects with a given key can be looked up.
	// It is strongly recommended to use the typed variants of this with NewIndex; this is needed to workaround Go type limitations.
	// If an index with the same name already exists, it is returned.
	Index(name string, extract func(o T) []string) RawIndexer
}

// RawIndexer is an internal-ish interface for indexes. Strongly recommended to use NewIndex.
type RawIndexer interface {
	Lookup(key string) []any
}

type Writer[T controllers.Object] interface {
	// Create creates a resource, returning the newly applied resource.
	Create(object T) (T, error)
	// Update updates a resource, returning the newly applied resource.
	Update(object T) (T, error)
	// UpdateStatus updates a resource's status, returning the newly applied resource.
	UpdateStatus(object T) (T, error)
	// Patch patches the resource, returning the newly applied resource.
	Patch(name, namespace string, pt apitypes.PatchType, data []byte) (T, error)
	// PatchStatus patches the resource's status, returning the newly applied resource.
	PatchStatus(name, namespace string, pt apitypes.PatchType, data []byte) (T, error)
	// ApplyStatus does a server-side Apply of the resource's status, returning the newly applied resource.
	// fieldManager is a required field; see https://kubernetes.io/docs/reference/using-api/server-side-apply/#managers.
	ApplyStatus(name, namespace string, pt apitypes.PatchType, data []byte, fieldManager string) (T, error)
	// Delete removes a resource.
	Delete(name, namespace string) error
}

type patcher[T controllers.Object] struct {
	Writer[T]
}

func (p patcher[T]) Patch(name, namespace string, pt apitypes.PatchType, data []byte) error {
	_, err := p.Writer.Patch(name, namespace, pt, data)
	return err
}

func (p patcher[T]) PatchStatus(name, namespace string, pt apitypes.PatchType, data []byte) error {
	_, err := p.Writer.PatchStatus(name, namespace, pt, data)
	return err
}

func (p patcher[T]) ApplyStatus(name, namespace string, pt apitypes.PatchType, data []byte, fieldManager string) error {
	_, err := p.Writer.ApplyStatus(name, namespace, pt, data, fieldManager)
	return err
}

func ToPatcher[T controllers.Object](w Writer[T]) Patcher {
	return patcher[T]{Writer: w}
}

// Patcher is a type-erased writer that can be used for patching
type Patcher interface {
	// Patch patches the resource, returning the newly applied resource.
	Patch(name, namespace string, pt apitypes.PatchType, data []byte) error
	// PatchStatus patches the resource's status, returning the newly applied resource.
	PatchStatus(name, namespace string, pt apitypes.PatchType, data []byte) error
	// ApplyStatus does a server-side Apply of the resource's status, returning the newly applied resource.
	// fieldManager is a required field; see https://kubernetes.io/docs/reference/using-api/server-side-apply/#managers.
	ApplyStatus(name, namespace string, pt apitypes.PatchType, data []byte, fieldManager string) error
}

type ReadWriter[T controllers.Object] interface {
	Reader[T]
	Writer[T]
}

// Client wraps a Kubernetes client providing cached read access and direct write access.
type Client[T controllers.Object] interface {
	Reader[T]
	Writer[T]
	Informer[T]
}
