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

package controller

import (
	"context"
	"time"

	"istio.io/istio/security/pkg/listwatch"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

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

	// Controller and store for namespace objects
	namespaceController cache.Controller
	namespaceStore      cache.Store
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(data func() map[string]string, options Options, kubeClient kubernetes.Interface) *NamespaceController {
	c := &NamespaceController{
		getData: data,
		client:  kubeClient.CoreV1(),
	}

	namespaceLW := listwatch.MultiNamespaceListerWatcher([]string{metav1.NamespaceAll}, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.Namespaces().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.Namespaces().Watch(context.TODO(), options)
			}}
	})

	c.namespaceStore, c.namespaceController =
		cache.NewInformer(namespaceLW, &v1.Namespace{}, namespaceResyncPeriod, cache.ResourceEventHandlerFuncs{
			UpdateFunc: c.namespaceUpdated,
			AddFunc:    c.namespaceAdded,
		})

	return c
}

// When a namespace is created, Citadel adds its public CA certificate
// to a well known configmap in the namespace.
func (nc *NamespaceController) namespaceAdded(obj interface{}) {
	ns, ok := obj.(*v1.Namespace)

	if ok {
		err := nc.insertDataForNamespace(ns.Name)
		if err != nil {
			log.Errorf("error when inserting CA cert to configmap: %v", err)
		} else {
			log.Debugf("inserted CA cert to configmap %v in ns %v",
				CACertNamespaceConfigMap, ns.GetName())
		}
	}
}

func (nc *NamespaceController) namespaceUpdated(oldObj, newObj interface{}) {
	ns, ok := newObj.(*v1.Namespace)

	if ok {
		// Every namespaceResyncPeriod, namespaceUpdated() will be invoked
		// for every namespace. If a namespace does not have the Citadel CA
		// certificate or the certificate in a ConfigMap of the namespace is not
		// up to date, Citadel updates the certificate in the namespace.
		// For simplifying the implementation and no overhead for reading the certificate from the ConfigMap,
		// simply updates the ConfigMap to the current Citadel CA certificate.
		err := nc.insertDataForNamespace(ns.Name)
		if err != nil {
			log.Errorf("error when updating CA cert in configmap: %v", err)
		} else {
			log.Debugf("updated CA cert in configmap %v in ns %v",
				CACertNamespaceConfigMap, ns.GetName())
		}
	}
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	go nc.namespaceController.Run(stopCh)
	log.Infof("Namespace controller started")
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
