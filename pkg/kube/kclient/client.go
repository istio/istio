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
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	istiogvr "istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kubeclient"
	types "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/informerfactory"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type fullClient[T controllers.Object] struct {
	writeClient[T]
	Informer[T]
}

type writeClient[T controllers.Object] struct {
	client kube.Client
}

// handlerRegistration stores a handler, with the registration so it can be de-registered
type handlerRegistration struct {
	registration cache.ResourceEventHandlerRegistration
	// handler is the actual handler. Note this does NOT have the filtering applied.
	handler cache.ResourceEventHandler
}

type informerClient[T controllers.Object] struct {
	informer      cache.SharedIndexInformer
	startInformer func(stopCh <-chan struct{})
	filter        func(t any) bool

	handlerMu          sync.RWMutex
	registeredHandlers []handlerRegistration
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

func (n *informerClient[T]) Start(stopCh <-chan struct{}) {
	n.startInformer(stopCh)
}

// internalIndex is the type we use to store a Kubernetes set of index, as well as the key to the index we care about.
// Users should not interact with this directly, and should instead use CreateIndex.
type internalIndex struct {
	key     string
	indexer cache.Indexer
	filter  func(t any) bool
}

func (i internalIndex) Lookup(key string) []any {
	res, err := i.indexer.ByIndex(i.key, key)
	if err != nil {
		// This should only happen if the index key (i.key, not key) does not exist which should be impossible.
		log.Fatalf("index lookup failed: %v", err)
	}
	if i.filter != nil {
		return slices.FilterInPlace(res, i.filter)
	}
	return res
}

var _ RawIndexer = internalIndex{}

func (n *informerClient[T]) Index(name string, extract func(o T) []string) RawIndexer {
	if _, ok := n.informer.GetIndexer().GetIndexers()[name]; !ok {
		if err := n.informer.AddIndexers(map[string]cache.IndexFunc{
			name: func(obj any) ([]string, error) {
				t := controllers.Extract[T](obj)
				return extract(t), nil
			},
		}); err != nil {
			// Should only happen on key conflict or on stop
			log.Warnf("failed to add indexer: %v", err)
		}
	}
	ret := internalIndex{
		key:     name,
		indexer: n.informer.GetIndexer(),
		filter:  n.filter,
	}
	return ret
}

func (n *writeClient[T]) Create(object T) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, object.GetNamespace())
	return api.Create(context.Background(), object, metav1.CreateOptions{})
}

func (n *writeClient[T]) Update(object T) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, object.GetNamespace())
	return api.Update(context.Background(), object, metav1.UpdateOptions{})
}

func (n *writeClient[T]) Patch(name, namespace string, pt apitypes.PatchType, data []byte) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, namespace)
	return api.Patch(context.Background(), name, pt, data, metav1.PatchOptions{})
}

func (n *writeClient[T]) PatchStatus(name, namespace string, pt apitypes.PatchType, data []byte) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, namespace)
	return api.Patch(context.Background(), name, pt, data, metav1.PatchOptions{}, "status")
}

func (n *writeClient[T]) ApplyStatus(name, namespace string, pt apitypes.PatchType, data []byte, fieldManager string) (T, error) {
	api := kubeclient.GetWriteClient[T](n.client, namespace)
	return api.Patch(context.Background(), name, pt, data, metav1.PatchOptions{
		Force:        ptr.Of(true),
		FieldManager: fieldManager,
	}, "status")
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
	n.handlerMu.Lock()
	defer n.handlerMu.Unlock()
	for _, c := range n.registeredHandlers {
		_ = n.informer.RemoveEventHandler(c.registration)
	}
	n.registeredHandlers = nil
}

func (n *informerClient[T]) ShutdownHandler(registration cache.ResourceEventHandlerRegistration) {
	n.handlerMu.Lock()
	defer n.handlerMu.Unlock()
	n.registeredHandlers = slices.FilterInPlace(n.registeredHandlers, func(h handlerRegistration) bool {
		return h.registration != registration
	})
	_ = n.informer.RemoveEventHandler(registration)
}

type neverReady struct{}

func (a neverReady) HasSynced() bool {
	return false
}

func (n *informerClient[T]) AddEventHandler(h cache.ResourceEventHandler) cache.ResourceEventHandlerRegistration {
	fh := cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			if n.filter == nil {
				return true
			}
			return n.filter(obj)
		},
		Handler: h,
	}
	n.handlerMu.Lock()
	defer n.handlerMu.Unlock()
	// AddEventHandler is safe to call under the lock. This will *enqueue* all existing items, but not block on processing them,
	// so the timing is quick.
	// If we do this outside the lock, we can hit a subtle race condition where we have started processing items before they
	// are registered (in n.registeredHandlers); this can cause the dynamic filtering to miss events
	reg, err := n.informer.AddEventHandler(fh)
	if err != nil {
		// Should only happen if its already stopped. We should exit early.
		return neverReady{}
	}
	n.registeredHandlers = append(n.registeredHandlers, handlerRegistration{registration: reg, handler: h})
	return reg
}

func (n *informerClient[T]) HasSynced() bool {
	if !n.informer.HasSynced() {
		return false
	}
	n.handlerMu.RLock()
	defer n.handlerMu.RUnlock()
	// HasSynced is fast, so doing it under the lock is okay
	for _, g := range n.registeredHandlers {
		if !g.registration.HasSynced() {
			return false
		}
	}
	return true
}

func (n *informerClient[T]) HasSyncedIgnoringHandlers() bool {
	return n.informer.HasSynced()
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
// Internally, this uses a shared informer, so calling this multiple times will share the same internals. This is keyed on
// unique {Type,LabelSelector,FieldSelector}.
//
// Warning: if conflicting filter.ObjectTransform are used for the same key, the first one registered wins.
// This means there must only be one filter configuration for a given type using the same kube.Client.
// Use with caution.
func NewFiltered[T controllers.ComparableObject](c kube.Client, filter Filter) Client[T] {
	gvr := types.MustToGVR[T](types.MustGVKFromType[T]())
	inf := kubeclient.GetInformerFiltered[T](c, ToOpts(c, gvr, filter), gvr)
	return &fullClient[T]{
		writeClient: writeClient[T]{client: c},
		Informer:    newInformerClient[T](gvr, inf, filter),
	}
}

// NewDelayedWriter returns a "delayed" client for the given GVR that is writeable.
// It is the client's responsibility to not write to the client if the type is not available; the "Delay" is not impacting
// the write flow. The reason for this is typically the writes are for status, which is in response to the object existing,
// which implies the GVR exists.
func NewDelayedWriter[T controllers.ComparableObject](
	c kube.Client,
	gvr schema.GroupVersionResource,
	filter Filter,
) Client[T] {
	inf := NewDelayedInformer[T](c, gvr, kubetypes.StandardInformer, filter)
	return &fullClient[T]{
		writeClient: writeClient[T]{client: c},
		Informer:    inf,
	}
}

// NewDelayedInformer returns a "delayed" client for the given GVR. This is read-only.
// A delayed client is used for CRD watches when the CRD may or may not exist. When the CRD is not present, the client will return
// empty results for all operations and watch for the CRD creation. Once created, watchers will be started and read operations will
// begin returning results.
// HasSynced will only return true if the CRD was not present upon creation OR the watch is fully synced. This ensures the creation
// is fully consistent if the CRD was present during creation; otherwise it is eventually consistent.
func NewDelayedInformer[T controllers.ComparableObject](
	c kube.Client,
	gvr schema.GroupVersionResource,
	informerType kubetypes.InformerType,
	filter Filter,
) Informer[T] {
	watcher := c.CrdWatcher()
	if watcher == nil {
		log.Fatal("NewDelayedInformer called without a CrdWatcher enabled")
	}
	delay := newDelayedFilter(gvr, watcher)
	inf := func() informerfactory.StartableInformer {
		opts := ToOpts(c, gvr, filter)
		opts.InformerType = informerType
		return kubeclient.GetInformerFiltered[T](c, opts, gvr)
	}
	return newDelayedInformer[T](gvr, inf, delay, filter)
}

// NewUntypedInformer returns an untyped client for a given GVR. This is read-only.
func NewUntypedInformer(c kube.Client, gvr schema.GroupVersionResource, filter Filter) Untyped {
	inf := kubeclient.GetInformerFilteredFromGVR(c, ToOpts(c, gvr, filter), gvr)
	return newInformerClient[controllers.Object](gvr, inf, filter)
}

// NewDynamic returns a dynamic client for a given GVR. This is read-only.
func NewDynamic(c kube.Client, gvr schema.GroupVersionResource, filter Filter) Untyped {
	opts := ToOpts(c, gvr, filter)
	opts.InformerType = kubetypes.DynamicInformer
	inf := kubeclient.GetInformerFilteredFromGVR(c, opts, gvr)
	return newInformerClient[controllers.Object](gvr, inf, filter)
}

// NewMetadata returns a metadata client for a given GVR. This is read-only.
func NewMetadata(c kube.Client, gvr schema.GroupVersionResource, filter Filter) Informer[*metav1.PartialObjectMetadata] {
	opts := ToOpts(c, gvr, filter)
	opts.InformerType = kubetypes.MetadataInformer
	inf := kubeclient.GetInformerFilteredFromGVR(c, opts, gvr)
	return newInformerClient[*metav1.PartialObjectMetadata](gvr, inf, filter)
}

// NewWriteClient is exposed for testing.
func NewWriteClient[T controllers.ComparableObject](c kube.Client) Writer[T] {
	return &writeClient[T]{client: c}
}

func newDelayedInformer[T controllers.ComparableObject](
	gvr schema.GroupVersionResource,
	getInf func() informerfactory.StartableInformer,
	delay kubetypes.DelayedFilter,
	filter Filter,
) Informer[T] {
	delayedClient := &delayedClient[T]{
		inf:     new(atomic.Pointer[Informer[T]]),
		delayed: delay,
	}

	// If resource is not yet known, we will use the delayedClient.
	// When the resource is later loaded, the callback will trigger and swap our dummy delayedClient
	// with a full client
	readyNow := delay.KnownOrCallback(func(stop <-chan struct{}) {
		// The inf() call is responsible for starting the informer
		inf := getInf()
		fc := &informerClient[T]{
			informer:      inf.Informer,
			startInformer: inf.Start,
		}
		applyDynamicFilter(filter, gvr, fc)
		inf.Start(stop)
		log.Infof("%v is now ready, building client", gvr.GroupResource())
		// Swap out the dummy client with the full one
		delayedClient.set(fc)
	})
	if !readyNow {
		log.Debugf("%v is not ready now, building delayed client", gvr.GroupResource())
		return delayedClient
	}
	log.Debugf("%v ready now, building client", gvr.GroupResource())
	return newInformerClient[T](gvr, getInf(), filter)
}

func newInformerClient[T controllers.ComparableObject](
	gvr schema.GroupVersionResource,
	inf informerfactory.StartableInformer,
	filter Filter,
) Informer[T] {
	ic := &informerClient[T]{
		informer:      inf.Informer,
		startInformer: inf.Start,
	}
	if filter.ObjectFilter != nil {
		applyDynamicFilter(filter, gvr, ic)
	}
	return ic
}

func applyDynamicFilter[T controllers.ComparableObject](filter Filter, gvr schema.GroupVersionResource, ic *informerClient[T]) {
	if filter.ObjectFilter != nil {
		ic.filter = filter.ObjectFilter.Filter
		filter.ObjectFilter.AddHandler(func(added, removed sets.String) {
			ic.handlerMu.RLock()
			defer ic.handlerMu.RUnlock()
			if gvr == istiogvr.Namespace {
				// Namespace is special; we query all namespaces
				// Note: other cluster-scoped resources should just not use the filter
				for _, item := range ic.ListUnfiltered(metav1.NamespaceAll, klabels.Everything()) {
					if !added.Contains(item.GetName()) {
						continue
					}
					for _, c := range ic.registeredHandlers {
						c.handler.OnAdd(item, false)
					}
				}
				// Removes are currently NOT handled. We only have the namespace name here. We would need to have the object
				// filter passthrough the entire namespace object, so we can pass the last known state to OnDelete.
				// Fortunately, missing a namespace delete event usually doesn't matter since everything in the namespace gets torn down.
			} else {
				for ns := range added {
					for _, item := range ic.ListUnfiltered(ns, klabels.Everything()) {
						for _, c := range ic.registeredHandlers {
							c.handler.OnAdd(item, false)
						}
					}
				}
				for ns := range removed {
					for _, item := range ic.ListUnfiltered(ns, klabels.Everything()) {
						for _, c := range ic.registeredHandlers {
							c.handler.OnDelete(item)
						}
					}
				}
			}
		})
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

func ToOpts(c kube.Client, gvr schema.GroupVersionResource, filter Filter) kubetypes.InformerOptions {
	ns := filter.Namespace
	if !istiogvr.IsClusterScoped(gvr) && ns == "" {
		ns = features.InformerWatchNamespace
	}
	return kubetypes.InformerOptions{
		LabelSelector:   filter.LabelSelector,
		FieldSelector:   filter.FieldSelector,
		Namespace:       ns,
		ObjectTransform: filter.ObjectTransform,
		Cluster:         c.ClusterID(),
	}
}
