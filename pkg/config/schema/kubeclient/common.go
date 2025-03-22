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

package kubeclient

import (
	"context"

	kubeext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/cache"
	gatewayapiinferenceclient "sigs.k8s.io/gateway-api-inference-extension/client-go/clientset/versioned"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/istio/pilot/pkg/util/informermetric"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/informerfactory"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/typemap"
)

type ClientGetter interface {
	// Ext returns the API extensions client.
	Ext() kubeext.Interface

	// Kube returns the core kube client
	Kube() kubernetes.Interface

	// Dynamic client.
	Dynamic() dynamic.Interface

	// Metadata returns the Metadata kube client.
	Metadata() metadata.Interface

	// Istio returns the Istio kube client.
	Istio() istioclient.Interface

	// GatewayAPI returns the gateway-api kube client.
	GatewayAPI() gatewayapiclient.Interface

	// GatewayAPIInference returns the gateway-api-inference-extension kube client.
	GatewayAPIInference() gatewayapiinferenceclient.Interface

	// Informers returns an informer factory.
	Informers() informerfactory.InformerFactory
}

// GetInformerFiltered attempts to use type information to setup the informer
// based on registered types. If no registered type is found, this will
// fallback to the same behavior as GetInformerFilteredFromGVR.
func GetInformerFiltered[T runtime.Object](
	c ClientGetter,
	opts ktypes.InformerOptions,
	gvr schema.GroupVersionResource,
) informerfactory.StartableInformer {
	reg := typemap.Get[TypeRegistration[T]](registerTypes)
	if reg != nil {
		// This is registered type
		tr := *reg
		return c.Informers().InformerFor(tr.GetGVR(), opts, func() cache.SharedIndexInformer {
			inf := cache.NewSharedIndexInformer(
				tr.ListWatch(c, opts),
				tr.Object(),
				0,
				cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			)
			setupInformer(opts, inf)
			return inf
		})
	}
	return GetInformerFilteredFromGVR(c, opts, gvr)
}

// GetInformerFilteredFromGVR will build an informer for the given GVR. When
// using ktypes.StandardInformer as the InformerType, the clients are selected
// from a statically defined list. Use GetInformerFiltered[T] for dynamically
// registered types.
func GetInformerFilteredFromGVR(c ClientGetter, opts ktypes.InformerOptions, g schema.GroupVersionResource) informerfactory.StartableInformer {
	switch opts.InformerType {
	case ktypes.DynamicInformer:
		return getInformerFilteredDynamic(c, opts, g)
	case ktypes.MetadataInformer:
		return getInformerFilteredMetadata(c, opts, g)
	default:
		return getInformerFiltered(c, opts, g)
	}
}

func getInformerFilteredDynamic(c ClientGetter, opts ktypes.InformerOptions, g schema.GroupVersionResource) informerfactory.StartableInformer {
	return c.Informers().InformerFor(g, opts, func() cache.SharedIndexInformer {
		inf := cache.NewSharedIndexInformerWithOptions(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return c.Dynamic().Resource(g).Namespace(opts.Namespace).List(context.Background(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return c.Dynamic().Resource(g).Namespace(opts.Namespace).Watch(context.Background(), options)
				},
			},
			&unstructured.Unstructured{},
			cache.SharedIndexInformerOptions{
				ResyncPeriod:      0,
				Indexers:          cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
				ObjectDescription: g.String(),
			},
		)
		setupInformer(opts, inf)
		return inf
	})
}

func getInformerFilteredMetadata(c ClientGetter, opts ktypes.InformerOptions, g schema.GroupVersionResource) informerfactory.StartableInformer {
	return c.Informers().InformerFor(g, opts, func() cache.SharedIndexInformer {
		inf := cache.NewSharedIndexInformerWithOptions(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return c.Metadata().Resource(g).Namespace(opts.Namespace).List(context.Background(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return c.Metadata().Resource(g).Namespace(opts.Namespace).Watch(context.Background(), options)
				},
			},
			&metav1.PartialObjectMetadata{},
			cache.SharedIndexInformerOptions{
				ResyncPeriod:      0,
				Indexers:          cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
				ObjectDescription: g.String(),
			},
		)
		setupInformer(opts, inf)
		return inf
	})
}

func setupInformer(opts ktypes.InformerOptions, inf cache.SharedIndexInformer) {
	// It is important to set this in the newFunc rather than after InformerFor to avoid
	// https://github.com/kubernetes/kubernetes/issues/117869
	if opts.ObjectTransform != nil {
		_ = inf.SetTransform(opts.ObjectTransform)
	} else {
		_ = inf.SetTransform(stripUnusedFields)
	}
	if err := inf.SetWatchErrorHandler(informermetric.ErrorHandlerForCluster(opts.Cluster)); err != nil {
		log.Debugf("failed to set watch handler, informer may already be started: %v", err)
	}
}

// stripUnusedFields is the transform function for shared informers,
// it removes unused fields from objects before they are stored in the cache to save memory.
func stripUnusedFields(obj any) (any, error) {
	t, ok := obj.(metav1.ObjectMetaAccessor)
	if !ok {
		// shouldn't happen
		return obj, nil
	}
	// ManagedFields is large and we never use it
	t.GetObjectMeta().SetManagedFields(nil)
	return obj, nil
}

var registerTypes = typemap.NewTypeMap()

// Register provides the TypeRegistration to the underlying
// store to enable dynamic object translation
func Register[T runtime.Object](
	gvr schema.GroupVersionResource,
	gvk schema.GroupVersionKind,
	list func(c ClientGetter, namespace string, o metav1.ListOptions) (runtime.Object, error),
	watch func(c ClientGetter, namespace string, o metav1.ListOptions) (watch.Interface, error),
) {
	reg := &internalTypeReg[T]{
		gvr:   gvr,
		gvk:   config.FromKubernetesGVK(gvk),
		list:  list,
		watch: watch,
	}
	kubetypes.Register[T](reg)
	typemap.Set[TypeRegistration[T]](registerTypes, reg)
}

// TypeRegistration represents the necessary methods
// to provide a custom type to the kubeclient informer mechanism
type TypeRegistration[T runtime.Object] interface {
	kubetypes.RegisterType[T]

	// ListWatchFunc provides the necessary methods for list and
	// watch for the informer
	ListWatch(c ClientGetter, opts ktypes.InformerOptions) cache.ListerWatcher
}

type internalTypeReg[T runtime.Object] struct {
	list  func(c ClientGetter, namespace string, o metav1.ListOptions) (runtime.Object, error)
	watch func(c ClientGetter, namespace string, o metav1.ListOptions) (watch.Interface, error)
	gvr   schema.GroupVersionResource
	gvk   config.GroupVersionKind
}

func (t *internalTypeReg[T]) GetGVK() config.GroupVersionKind {
	return t.gvk
}

func (t *internalTypeReg[T]) GetGVR() schema.GroupVersionResource {
	return t.gvr
}

func (t *internalTypeReg[T]) ListWatch(c ClientGetter, o ktypes.InformerOptions) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = o.FieldSelector
			options.LabelSelector = o.LabelSelector
			return t.list(c, o.Namespace, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = o.FieldSelector
			options.LabelSelector = o.LabelSelector
			return t.watch(c, o.Namespace, options)
		},
	}
}

func (t *internalTypeReg[T]) Object() T {
	return ptr.Empty[T]()
}
