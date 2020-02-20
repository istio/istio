// Copyright 2020 Istio Authors
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

package bootstrap

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/queue"
	"istio.io/istio/security/pkg/listwatch"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	certutil "istio.io/istio/security/pkg/util"
)

const (
	// Every namespaceResyncPeriod, namespaceUpdated() will be invoked
	// for every namespace. This value must be configured so Citadel
	// can update its CA certificate in a ConfigMap in every namespace.
	namespaceResyncPeriod = time.Second * 30
	// The name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap = "istio-ca-root-cert"
)

var (
	configMapLabel = map[string]string{"istio.io/config": "true"}
)

// NamespaceController manages reconciles a configmap in each namespace with a desired set of data.
type NamespaceController struct {
	// getData is the function to fetch the data we will insert into the config map
	getData func() map[string]string
	core    corev1.CoreV1Interface

	queue queue.Instance

	// Controller and store for namespace objects
	namespaceController cache.Controller
	// Controller and store for ConfigMap objects
	configMapController cache.Controller
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(data func() map[string]string, core corev1.CoreV1Interface) (*NamespaceController, error) {
	c := &NamespaceController{
		getData: data,
		core:    core,
		queue:   queue.NewQueue(time.Second),
	}
	configMapLw := listwatch.MultiNamespaceListerWatcher([]string{metav1.NamespaceAll}, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = fields.SelectorFromSet(configMapLabel).String()
				return core.ConfigMaps(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = fields.SelectorFromSet(configMapLabel).String()
				return core.ConfigMaps(namespace).Watch(options)
			}}
	})
	_, c.configMapController =
		cache.NewInformer(configMapLw, &v1.ConfigMap{}, 0, cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				c.queue.Push(func() error {
					return c.configMapChange(newObj)
				})
			},
			DeleteFunc: func(obj interface{}) {
				cm, ok := obj.(*v1.ConfigMap)
				if !ok {
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						log.Errorf("error decoding object, invalid type")
						return
					}
					cm, ok = tombstone.Obj.(*v1.ConfigMap)
					if !ok {
						log.Errorf("error decoding object tombstone, invalid type")
						return
					}
				}
				c.queue.Push(func() error {
					ns, err := core.Namespaces().Get(cm.Namespace, metav1.GetOptions{})
					if err != nil {
						return err
					}
					// If the namespace is terminating, we may get into a loop of trying to re-add the configmap back
					// We should make sure the namespace still exists
					if ns.Status.Phase != v1.NamespaceTerminating {
						return c.insertDataForNamespace(cm.Namespace)
					}
					return nil
				})
			},
		})

	namespaceLW := listwatch.MultiNamespaceListerWatcher([]string{metav1.NamespaceAll}, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return core.Namespaces().List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return core.Namespaces().Watch(options)
			}}
	})
	_, c.namespaceController =
		cache.NewInformer(namespaceLW, &v1.Namespace{}, 0, cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(_, newObj interface{}) {
				c.queue.Push(func() error {
					return c.namespaceChange(newObj)
				})
			},
			AddFunc: func(obj interface{}) {
				c.queue.Push(func() error {
					return c.namespaceChange(obj)
				})
			},
		})
	return c, nil
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	go nc.namespaceController.Run(stopCh)
	go nc.configMapController.Run(stopCh)
	cache.WaitForCacheSync(stopCh, nc.namespaceController.HasSynced, nc.configMapController.HasSynced)
	log.Infof("Namespace controller started")
	go nc.queue.Run(stopCh)
}

// insertDataForNamespace will add data into the configmap for the specified namespace
// If the configmap is not found, it will be created.
// If you know the current contents of the configmap, using UpdateDataInConfigMap is more efficient.
func (nc *NamespaceController) insertDataForNamespace(ns string) error {
	meta := metav1.ObjectMeta{
		Name:      CACertNamespaceConfigMap,
		Namespace: ns,
		Labels:    configMapLabel,
	}
	return certutil.InsertDataToConfigMap(nc.core, meta, nc.getData())
}

// On namespace change, update the config map.
// If terminating, this will be skipped
func (nc *NamespaceController) namespaceChange(obj interface{}) error {
	ns, ok := obj.(*v1.Namespace)

	if ok && ns.Status.Phase != v1.NamespaceTerminating {
		return nc.insertDataForNamespace(ns.Name)
	}
	return nil
}

// When a config map is changed, merge the data into the configmap
func (nc *NamespaceController) configMapChange(obj interface{}) error {
	cm, ok := obj.(*v1.ConfigMap)

	if ok {
		if err := certutil.UpdateDataInConfigMap(nc.core, cm.DeepCopy(), nc.getData()); err != nil {
			return fmt.Errorf("error when inserting CA cert to configmap %v: %v", cm.Name, err)
		}
	}
	return nil
}
