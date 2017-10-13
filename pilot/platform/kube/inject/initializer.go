// Copyright 2017 Istio Authors
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

package inject

import (
	"encoding/json"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	v2alpha1 "k8s.io/api/batch/v2alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
	"istio-system",
}

// Initializer implements a k8s initializer for transparently
// injecting the sidecar into user resources. For each resource in the
// managed namespace, the initializer will remove itself from the
// pending list of initializers can optionally inject the sidecar
// based on the InjectionPolicy and per-resource policy (see
// istioSidecarAnnotationPolicyKey).
type Initializer struct {
	clientset   kubernetes.Interface
	controllers []cache.Controller
	config      *Config
}

var (
	kinds = []struct {
		groupVersion schema.GroupVersion
		obj          runtime.Object
		resource     string
		apiPath      string
	}{
		{v1.SchemeGroupVersion, &v1.ReplicationController{}, "replicationcontrollers", "/api"},

		{v1beta1.SchemeGroupVersion, &v1beta1.Deployment{}, "deployments", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.DaemonSet{}, "daemonsets", "/apis"},
		{v1beta1.SchemeGroupVersion, &v1beta1.ReplicaSet{}, "replicasets", "/apis"},

		{batchv1.SchemeGroupVersion, &batchv1.Job{}, "jobs", "/apis"},
		{v2alpha1.SchemeGroupVersion, &v2alpha1.CronJob{}, "cronjobs", "/apis"},
		// TODO JobTemplate requires different reflection logic to populate the PodTemplateSpec

		{appsv1beta1.SchemeGroupVersion, &appsv1beta1.StatefulSet{}, "statefulsets", "/apis"},
	}
	injectScheme = runtime.NewScheme()
)

type patcherFunc func(namespace, name string, patchBytes []byte, obj runtime.Object) error

func init() {
	for _, kind := range kinds {
		injectScheme.AddKnownTypes(kind.groupVersion, kind.obj)
		injectScheme.AddUnversionedTypes(kind.groupVersion, kind.obj)
	}
}

// NewInitializer creates a new instance of the Istio sidecar initializer.
func NewInitializer(restConfig *rest.Config, config *Config, cl kubernetes.Interface) (*Initializer, error) {
	i := &Initializer{
		clientset: cl,
		config:    config,
	}

	for k := range kinds {
		kind := kinds[k]

		// Create RESTClient for the specific GroupVersion and APIPath.
		kindConfig := *restConfig
		kindConfig.GroupVersion = &kind.groupVersion
		kindConfig.APIPath = kind.apiPath
		kindConfig.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
		if kindConfig.UserAgent == "" {
			kindConfig.UserAgent = rest.DefaultKubernetesUserAgent()
		}
		kindClient, err := rest.RESTClientFor(&kindConfig)
		if err != nil {
			return nil, err
		}

		for n := range i.config.IncludeNamespaces {
			namespace := i.config.IncludeNamespaces[n]

			watchlist := &cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.IncludeUninitialized = true
					options.FieldSelector = fields.Everything().String()
					return kindClient.Get().
						Namespace(namespace).
						Resource(kind.resource).
						VersionedParams(&options, metav1.ParameterCodec).
						Do().
						Get()
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.IncludeUninitialized = true
					options.Watch = true
					options.FieldSelector = fields.Everything().String()
					return kindClient.Get().
						Namespace(namespace).
						Resource(kind.resource).
						VersionedParams(&options, metav1.ParameterCodec).
						Watch()
				},
			}

			patcher := func(namespace, name string, patchBytes []byte, obj runtime.Object) error {
				return kindClient.Patch(types.StrategicMergePatchType).
					Namespace(namespace).
					Resource(kind.resource).
					Name(name).
					Body(patchBytes).
					Do().
					Into(obj)
			}

			_, controller := cache.NewInformer(watchlist, kind.obj, DefaultResyncPeriod,
				cache.ResourceEventHandlerFuncs{
					AddFunc: func(obj interface{}) {
						if err := i.initialize(obj, patcher); err != nil {
							glog.Error(err.Error())
						}
					},
				},
			)
			i.controllers = append(i.controllers, controller)
		}
	}
	return i, nil
}

func (i *Initializer) initialize(in interface{}, patcher patcherFunc) error {
	obj, err := meta.Accessor(in)
	if err != nil {
		return err
	}

	var inject bool
namespaceSearch:
	for _, namespace := range i.config.IncludeNamespaces {
		if namespace == v1.NamespaceAll {
			inject = true
			break namespaceSearch
		} else if namespace == obj.GetNamespace() {
			// Don't skip. The initializer should initialize this
			// resource.
			inject = true
			break namespaceSearch
		}
		// else, keep searching
	}
	if !inject {
		// Skip namespace(s) that we're not responsible for if
		// len(pendingInitializers) == 0 { initializing.
		glog.V(2).Infof("Skipping %s/%s: non-managed namespace",
			obj.GetNamespace(), obj.GetName())
		return nil
	}

	gvk, _, err := injectScheme.ObjectKind(in.(runtime.Object))
	if err != nil {
		return err
	}

	glog.V(2).Infof("ObjectMeta initializer info %v %v/%v policy:%q status:%q %v",
		gvk, obj.GetNamespace(), obj.GetName(),
		obj.GetAnnotations()[istioSidecarAnnotationPolicyKey],
		obj.GetAnnotations()[istioSidecarAnnotationStatusKey],
		obj.GetInitializers())

	if obj.GetInitializers() == nil {
		return nil
	}
	pendingInitializers := obj.GetInitializers().Pending
	if len(pendingInitializers) == 0 {
		return nil
	}
	if i.config.InitializerName != pendingInitializers[0].Name {
		return nil
	}

	out, err := intoObject(i.config, in)
	if err != nil {
		return err
	}

	if obj, err = meta.Accessor(out); err != nil {
		return err
	}

	// Remove self from the list of pending Initializers while
	// preserving ordering.
	if pending := obj.GetInitializers().Pending; len(pending) == 1 {
		obj.SetInitializers(nil)
	} else {
		obj.GetInitializers().Pending = append(pending[:0], pending[1:]...)
	}

	prevData, err := json.Marshal(in)
	if err != nil {
		return err
	}
	currData, err := json.Marshal(out)
	if err != nil {
		return err
	}
	rObj, err := injectScheme.New(gvk)
	if err != nil {
		return err
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(prevData, currData, rObj)
	if err != nil {
		return err
	}
	return patcher(obj.GetNamespace(), obj.GetName(), patchBytes, rObj)
}

// Run runs the Initializer controller.
func (i *Initializer) Run(stopCh <-chan struct{}) {
	glog.Info("Starting Istio sidecar initializer...")
	glog.Infof("Initializer name set to: %s", i.config.InitializerName)
	glog.Infof("Options: %v", spew.Sdump(i.config))

	glog.Infof("Supported kinds:")
	for _, kind := range kinds {
		if gvk, _, err := injectScheme.ObjectKind(kind.obj); err != nil {
			glog.Warningf("Could not determine object kind: ", err)
		} else {
			glog.Infof("\t%v/%v %v", gvk.Group, gvk.Version, gvk.Kind)
		}
	}

	for _, controller := range i.controllers {
		go controller.Run(stopCh)
	}
}
