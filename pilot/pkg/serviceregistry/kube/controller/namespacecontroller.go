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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/listwatch"
	"istio.io/istio/pkg/queue"
	certutil "istio.io/istio/security/pkg/util"
)

const (
	// Every NamespaceResyncPeriod, namespaceUpdated() will be invoked
	// for every namespace. This value must be configured so Citadel
	// can update its CA certificate in a ConfigMap in every namespace.
	NamespaceResyncPeriod = time.Second * 60
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
	client  corev1.CoreV1Interface

	queue queue.Instance

	// Controller and store for namespace objects
	namespaceController cache.Controller
	// Controller and store for ConfigMap objects
	configMapController cache.Controller
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(data func() map[string]string, options Options, kubeClient kubernetes.Interface) *NamespaceController {
	c := &NamespaceController{
		getData: data,
		client:  kubeClient.CoreV1(),
		queue:   queue.NewQueue(time.Second),
	}

	watchedNamespaceList := strings.Split(options.WatchedNamespaces, ",")

	mlw := listwatch.MultiNamespaceListerWatcher(watchedNamespaceList, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = fields.SelectorFromSet(configMapLabel).String()
				return kubeClient.CoreV1().ConfigMaps(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = fields.SelectorFromSet(configMapLabel).String()
				return kubeClient.CoreV1().ConfigMaps(namespace).Watch(context.TODO(), opts)
			},
		}
	})

	configmapInformer := cache.NewSharedIndexInformer(mlw, &v1.ConfigMap{}, options.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	configmapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
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
				ns, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), cm.Namespace, metav1.GetOptions{})
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
	c.configMapController = configmapInformer

	namespaceInformer := informer.NewNamespaceInformer(kubeClient, options.ResyncPeriod, cache.Indexers{})
	namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.queue.Push(func() error {
				return c.namespaceChange(obj)
			})
		},
		UpdateFunc: func(_, obj interface{}) {
			c.queue.Push(func() error {
				return c.namespaceChange(obj)
			})
		},
	})
	c.namespaceController = namespaceInformer

	return c
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
	return certutil.InsertDataToConfigMap(nc.client, meta, nc.getData())
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
		if err := certutil.UpdateDataInConfigMap(nc.client, cm.DeepCopy(), nc.getData()); err != nil {
			return fmt.Errorf("error when inserting CA cert to configmap %v: %v", cm.Name, err)
		}
	}
	return nil
}
