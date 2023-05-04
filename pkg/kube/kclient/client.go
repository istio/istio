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
	"context"
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/informermetric"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/ptr"
	"istio.io/pkg/log"
)

type fullClient[T controllers.Object] struct {
	writeClient[T]
	informerClient[T]
}

type writeClient[T controllers.Object] struct {
	client kube.Client
}

type informerClient[T controllers.Object] struct {
	informer           cache.SharedIndexInformer
	filter             func(t any) bool
	registeredHandlers []cache.ResourceEventHandlerRegistration
}

func (n *informerClient[T]) Get(name, namespace string) T {
	obj, exists, err := n.informer.GetIndexer().GetByKey(keyFunc(name, namespace))
	if err != nil {
		return ptr.Empty[T]()
	}
	if !exists {
		return ptr.Empty[T]()
	}
	cast := obj.(T)
	if !n.applyFilter(cast) {
		return ptr.Empty[T]()
	}
	return cast
}

func (n *informerClient[T]) applyFilter(t T) bool {
	if n.filter == nil {
		return true
	}
	return n.filter(t)
}

func (n *writeClient[T]) Create(object T) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, object.GetNamespace())
	return api.Create(context.Background(), object, metav1.CreateOptions{})
}

func (n *writeClient[T]) Update(object T) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, object.GetNamespace())
	return api.Update(context.Background(), object, metav1.UpdateOptions{})
}

func (n *writeClient[T]) UpdateStatus(object T) (T, error) {
	api, ok := kubeclient.GetWriteClient[T](n.client, object.GetNamespace()).(kubetypes.WriteStatusAPI[T])
	if !ok {
		return ptr.Empty[T](), fmt.Errorf("%T does not support UpdateStatus", object)
	}
	return api.UpdateStatus(context.Background(), object, metav1.UpdateOptions{})
}

func (n *writeClient[T]) Delete(name, namespace string) error {
	api := kubeclient.GetWriteClient[T](n.client, namespace)
	return api.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func (n *informerClient[T]) ShutdownHandlers() {
	for _, c := range n.registeredHandlers {
		_ = n.informer.RemoveEventHandler(c)
	}
}

func (n *informerClient[T]) AddEventHandler(h cache.ResourceEventHandler) {
	fh := cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			if n.filter == nil {
				return true
			}
			return n.filter(obj)
		},
		Handler: h,
	}
	reg, _ := n.informer.AddEventHandler(fh)
	n.registeredHandlers = append(n.registeredHandlers, reg)
}

func (n *informerClient[T]) HasSynced() bool {
	if !n.informer.HasSynced() {
		return false
	}
	for _, g := range n.registeredHandlers {
		if !g.HasSynced() {
			return false
		}
	}
	return true
}

func (n *informerClient[T]) List(namespace string, selector klabels.Selector) []T {
	var res []T
	err := cache.ListAllByNamespace(n.informer.GetIndexer(), namespace, selector, func(i any) {
		cast := i.(T)
		if n.applyFilter(cast) {
			res = append(res, cast)
		}
	})

	// Should never happen
	if err != nil && features.EnableUnsafeAssertions {
		log.Fatalf("lister returned err for %v: %v", namespace, err)
	}
	return res
}

func (n *informerClient[T]) ListUnfiltered(namespace string, selector klabels.Selector) []T {
	var res []T
	err := cache.ListAllByNamespace(n.informer.GetIndexer(), namespace, selector, func(i any) {
		cast := i.(T)
		res = append(res, cast)
	})

	// Should never happen
	if err != nil && features.EnableUnsafeAssertions {
		log.Fatalf("lister returned err for %v: %v", namespace, err)
	}
	return res
}

// Filter allows filtering read operations.
// This is aliased to allow easier access when constructing clients.
type Filter = kubetypes.Filter

// New returns a Client for the given type.
// Internally, this uses a shared informer, so calling this multiple times will share the same internals.
func New[T controllers.ComparableObject](c kube.Client) Client[T] {
	return NewFiltered[T](c, Filter{})
}

// NewFiltered returns a Client with some filter applied.
// Internally, this uses a shared informer, so calling this multiple times will share the same internals.
//
// Warning: currently, if filter.LabelSelector or filter.FieldSelector are set, the same informer will still be used
// This means there must only be one filter configuration for a given type using the same kube.Client (generally, this means the whole program).
// Use with caution.
func NewFiltered[T controllers.ComparableObject](c kube.Client, filter Filter) Client[T] {
	var inf cache.SharedIndexInformer
	if filter.LabelSelector == "" && filter.FieldSelector == "" {
		inf = kubeclient.GetInformer[T](c)
	} else {
		inf = kubeclient.GetInformerFiltered[T](c, kubetypes.InformerOptions{
			LabelSelector: filter.LabelSelector,
			FieldSelector: filter.FieldSelector,
		})
	}

	return &fullClient[T]{
		writeClient:    writeClient[T]{client: c},
		informerClient: newInformerClient[T](c, inf, filter),
	}
}

// NewUntyped returns an untyped client for a given informer. This is read-only.
//
// Warning: because the informer is already created, only client side filters are supported.
func NewUntyped(c kube.Client, inf cache.SharedIndexInformer, filter Filter) Untyped {
	if filter.LabelSelector != "" {
		panic("invalid filter supplied, LabelSelector not allowed")
	}
	if filter.FieldSelector != "" {
		panic("invalid filter supplied, FieldSelector not allowed")
	}
	return ptr.Of(newInformerClient[controllers.Object](c, inf, filter))
}

// NewWriteClient is exposed for testing.
func NewWriteClient[T controllers.ComparableObject](c kube.Client) Writer[T] {
	return &writeClient[T]{client: c}
}

func newInformerClient[T controllers.ComparableObject](c kube.Client, inf cache.SharedIndexInformer, filter Filter) informerClient[T] {
	i := *new(T)
	t := reflect.TypeOf(i)
	if err := c.RegisterFilter(t, filter); err != nil {
		if features.EnableUnsafeAssertions {
			log.Fatal(err)
		}
		log.Warn(err)
	}
	if filter.ObjectTransform != nil {
		_ = inf.SetTransform(filter.ObjectTransform)
	} else {
		_ = inf.SetTransform(kube.StripUnusedFields)
	}

	if err := inf.SetWatchErrorHandler(informermetric.ErrorHandlerForCluster(c.ClusterID())); err != nil {
		log.Debugf("failed to set watch handler, informer may already be started: %v", err)
	}
	return informerClient[T]{
		informer: inf,
		filter:   filter.ObjectFilter,
	}
}

// keyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func keyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}
