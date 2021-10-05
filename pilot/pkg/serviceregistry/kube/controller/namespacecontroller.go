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
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/security/pkg/k8s"
)

const (
	// NamespaceResyncPeriod : every NamespaceResyncPeriod, namespaceUpdated() will be invoked
	// for every namespace. This value must be configured so Citadel
	// can update its CA certificate in a ConfigMap in every namespace.
	NamespaceResyncPeriod = time.Second * 60
	// CACertNamespaceConfigMap is the name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap = "istio-ca-root-cert"
)

var configMapLabel = map[string]string{"istio.io/config": "true"}

// NamespaceController manages reconciles a configmap in each namespace with a desired set of data.
type NamespaceController struct {
	// getData is the function to fetch the data we will insert into the config map
	getData func() map[string]string
	client  corev1.CoreV1Interface

	queue              workqueue.RateLimitingInterface
	namespacesInformer cache.SharedInformer
	configMapInformer  cache.SharedInformer
	namespaceLister    listerv1.NamespaceLister
	configmapLister    listerv1.ConfigMapLister
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(data func() map[string]string, kubeClient kube.Client) *NamespaceController {
	c := &NamespaceController{
		getData: data,
		client:  kubeClient.CoreV1(),
		queue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	c.configMapInformer = kubeClient.KubeInformer().Core().V1().ConfigMaps().Informer()
	c.configmapLister = kubeClient.KubeInformer().Core().V1().ConfigMaps().Lister()
	c.namespacesInformer = kubeClient.KubeInformer().Core().V1().Namespaces().Informer()
	c.namespaceLister = kubeClient.KubeInformer().Core().V1().Namespaces().Lister()

	c.configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(_, obj interface{}) {
			c.configMapChange(obj)
		},
		DeleteFunc: func(obj interface{}) {
			c.configMapChange(obj)
		},
	})

	c.namespacesInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.namespaceChange(obj.(*v1.Namespace))
		},
		UpdateFunc: func(_, obj interface{}) {
			c.namespaceChange(obj.(*v1.Namespace))
		},
	})

	return c
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	defer nc.queue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, nc.namespacesInformer.HasSynced, nc.configMapInformer.HasSynced) {
		log.Error("Failed to sync namespace controller cache")
		return
	}
	log.Infof("Namespace controller started")

	go wait.Until(nc.runWorker, time.Second, stopCh)

	<-stopCh
}

func (nc *NamespaceController) runWorker() {
	for nc.processNextWorkItem() {
	}
}

// processNextWorkItem deals with one key off the queue. It returns false when
// it's time to quit.
func (nc *NamespaceController) processNextWorkItem() bool {
	key, quit := nc.queue.Get()
	if quit {
		return false
	}
	defer nc.queue.Done(key)

	if err := nc.insertDataForNamespace(key.(string)); err != nil {
		utilruntime.HandleError(fmt.Errorf("insertDataForNamespace %q failed: %v", key, err))
		nc.queue.AddRateLimited(key)
		return true
	}

	nc.queue.Forget(key)
	return true
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
	return k8s.InsertDataToConfigMap(nc.client, nc.configmapLister, meta, nc.getData())
}

// On namespace change, update the config map.
// If terminating, this will be skipped
func (nc *NamespaceController) namespaceChange(ns *v1.Namespace) {
	if ns.Status.Phase != v1.NamespaceTerminating {
		nc.syncNamespace(ns.Name)
	}
}

// On configMap change(update or delete), try to create or update the config map.
func (nc *NamespaceController) configMapChange(obj interface{}) {
	cm, err := convertToConfigMap(obj)
	if err != nil {
		log.Errorf("failed to convert to configmap: %v", err)
		return
	}
	// This is a change to a configmap we don't watch, ignore it
	if cm.Name != CACertNamespaceConfigMap {
		return
	}
	nc.syncNamespace(cm.Namespace)
}

func (nc *NamespaceController) syncNamespace(ns string) {
	// skip special kubernetes system namespaces
	for _, namespace := range inject.IgnoredNamespaces {
		if ns == namespace {
			return
		}
	}
	nc.queue.Add(ns)
}

func convertToConfigMap(obj interface{}) (*v1.ConfigMap, error) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil, fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		cm, ok = tombstone.Obj.(*v1.ConfigMap)
		if !ok {
			return nil, fmt.Errorf("tombstone contained object that is not a ConfigMap %#v", obj)
		}
	}
	return cm, nil
}
