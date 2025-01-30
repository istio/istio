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

package kubetypes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type InformerOptions struct {
	// A selector to restrict the list of returned objects by their labels.
	LabelSelector string
	// A selector to restrict the list of returned objects by their fields.
	FieldSelector string
	// Namespace to watch.
	Namespace string
	// Cluster name for watch error handlers
	Cluster cluster.ID
	// ObjectTransform allows arbitrarily modifying objects stored in the underlying cache.
	// If unset, a default transform is provided to remove ManagedFields (high cost, low value)
	ObjectTransform func(obj any) (any, error)
	// InformerType dictates the type of informer that should be created.
	InformerType InformerType
}

type InformerType int

const (
	StandardInformer InformerType = iota
	DynamicInformer
	MetadataInformer
)

type DynamicObjectFilter interface {
	// Filter returns true if the input object or namespace string resides in a namespace selected for discovery
	Filter(obj any) bool
	// AddHandler registers a handler on namespace, which will be triggered when namespace selected or deselected.
	AddHandler(func(selected, deselected sets.String))
}

type staticFilter struct {
	f func(obj interface{}) bool
}

func (s staticFilter) Filter(obj any) bool {
	return s.f(obj)
}

func (s staticFilter) AddHandler(func(selected, deselected sets.String)) {
	// Do nothing
}

var _ DynamicObjectFilter = staticFilter{}

// NewStaticObjectFilter returns a DynamicObjectFilter that does not ever change (so does not need an AddHandler)
func NewStaticObjectFilter(f func(obj any) bool) DynamicObjectFilter {
	return staticFilter{f}
}

// Filter allows filtering read operations
type Filter struct {
	// A selector to restrict the list of returned objects by their labels.
	// This is a *server side* filter.
	LabelSelector string
	// A selector to restrict the list of returned objects by their fields.
	// This is a *server side* filter.
	FieldSelector string
	// Namespace to watch.
	// This is a *server side* filter.
	Namespace string
	// ObjectFilter allows arbitrary filtering logic.
	// This is a *client side* filter. This means CPU/memory costs are still present for filtered objects.
	// Use LabelSelector or FieldSelector instead, if possible.
	ObjectFilter DynamicObjectFilter
	// ObjectTransform allows arbitrarily modifying objects stored in the underlying cache.
	// If unset, a default transform is provided to remove ManagedFields (high cost, low value)
	ObjectTransform func(obj any) (any, error)
}

// composedFilter offers a way to join multiple different object filters into a single one
type composedFilter struct {
	// The primary filter, which has a handler. Optional
	filter DynamicObjectFilter
	// Secondary filters (no handler allowed)
	extra []func(obj any) bool
}

func (f composedFilter) Filter(obj any) bool {
	for _, filter := range f.extra {
		if !filter(obj) {
			return false
		}
	}
	if f.filter != nil {
		return f.filter.Filter(obj)
	}
	return true
}

func (f composedFilter) AddHandler(fn func(selected, deselected sets.String)) {
	if f.filter != nil {
		f.filter.AddHandler(fn)
	}
}

func ComposeFilters(filter DynamicObjectFilter, extra ...func(obj any) bool) DynamicObjectFilter {
	return composedFilter{
		filter: filter,
		extra: slices.FilterInPlace(extra, func(f func(obj any) bool) bool {
			return f != nil
		}),
	}
}

// CrdWatcher exposes an interface to watch CRDs
type CrdWatcher interface {
	// HasSynced returns true once all existing state has been synced.
	HasSynced() bool
	// KnownOrCallback returns `true` immediately if the resource is known.
	// If it is not known, `false` is returned. If the resource is later added, the callback will be triggered.
	KnownOrCallback(s schema.GroupVersionResource, f func(stop <-chan struct{})) bool
	// WaitForCRD waits until the request CRD exists, and returns true on success. A false return value
	// indicates the CRD does not exist but the wait failed or was canceled.
	// This is useful to conditionally enable controllers based on CRDs being created.
	WaitForCRD(s schema.GroupVersionResource, stop <-chan struct{}) bool
	// Run starts the controller. This must be called.
	Run(stop <-chan struct{})
}

// DelayedFilter exposes an interface for a filter create delayed informers, which start
// once the underlying resource is available. See kclient.NewDelayedInformer.
type DelayedFilter interface {
	HasSynced() bool
	KnownOrCallback(f func(stop <-chan struct{})) bool
}

// WriteAPI exposes a generic API for a client go type for write operations.
type WriteAPI[T runtime.Object] interface {
	Create(ctx context.Context, object T, opts metav1.CreateOptions) (T, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result T, err error)
	Update(ctx context.Context, object T, opts metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

// WriteAPI exposes a generic API for a client go type for status operations.
// Not all types have status, so they need to be split out
type WriteStatusAPI[T runtime.Object] interface {
	UpdateStatus(ctx context.Context, object T, opts metav1.UpdateOptions) (T, error)
}

// ReadAPI exposes a generic API for a client go type for read operations.
type ReadAPI[T runtime.Object, TL runtime.Object] interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (T, error)
	List(ctx context.Context, opts metav1.ListOptions) (TL, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

// ReadWriteAPI exposes a generic API for read and write operations.
type ReadWriteAPI[T runtime.Object, TL runtime.Object] interface {
	ReadAPI[T, TL]
	WriteAPI[T]
}

// ApplyAPI exposes a generic API for a client go type for apply operations.
type ApplyAPI[T runtime.Object, TA runtime.Object] interface {
	Apply(ctx context.Context, secret TA, opts metav1.ApplyOptions) (result T, err error)
}

// FullAPI exposes a generic API for a client go type for all operations.
// Note each type can also have per-type specific "Expansions" not covered here.
type FullAPI[T runtime.Object, TL runtime.Object, TA runtime.Object] interface {
	ReadAPI[T, TL]
	WriteAPI[T]
	ApplyAPI[T, TA]
}
