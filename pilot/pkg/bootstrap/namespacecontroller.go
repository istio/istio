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
	"time"

	"istio.io/istio/pkg/config/constants"

	"istio.io/istio/security/pkg/pki/ca"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/security/pkg/listwatch"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

const (
	// Every namespaceResyncPeriod, namespaceUpdated() will be invoked
	// for every namespace. This value must be configured so Citadel
	// can update its CA certificate in a ConfigMap in every namespace.
	namespaceResyncPeriod = time.Second * 30
	// The name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap      = "istio-ca-root-cert"
	CACertNamespaceInsertInterval = time.Second
	CACertNamespaceInsertTimeout  = time.Second * 2
)

// NamespaceController manages the CA certificate in each namespace.
type NamespaceController struct {
	ca   *ca.IstioCA
	core corev1.CoreV1Interface

	// Controller and store for namespace objects
	namespaceController cache.Controller
	namespaceStore      cache.Store
}

// NewNamespaceController returns a pointer to a newly constructed SecretController instance.
func NewNamespaceController(ca *ca.IstioCA, core corev1.CoreV1Interface) (*NamespaceController, error) {
	c := &NamespaceController{
		ca:   ca,
		core: core,
	}

	namespaceLW := listwatch.MultiNamespaceListerWatcher([]string{metav1.NamespaceAll}, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return core.Namespaces().List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return core.Namespaces().Watch(options)
			}}
	})
	c.namespaceStore, c.namespaceController =
		cache.NewInformer(namespaceLW, &v1.Namespace{}, namespaceResyncPeriod, cache.ResourceEventHandlerFuncs{
			UpdateFunc: c.namespaceUpdated,
			AddFunc:    c.namespaceAdded,
		})
	return c, nil
}

// When a namespace is created, Citadel adds its public CA certificate
// to a well known configmap in the namespace.
func (nc *NamespaceController) namespaceAdded(obj interface{}) {
	ns, ok := obj.(*v1.Namespace)

	if ok {
		rootCert := nc.ca.GetCAKeyCertBundle().GetRootCertPem()
		err := certutil.InsertDataToConfigMapWithRetry(nc.core, ns.GetName(), string(rootCert), CACertNamespaceConfigMap,
			constants.CACertNamespaceConfigMapDataName, CACertNamespaceInsertInterval, CACertNamespaceInsertTimeout)
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
		rootCert := nc.ca.GetCAKeyCertBundle().GetRootCertPem()
		// Every namespaceResyncPeriod, namespaceUpdated() will be invoked
		// for every namespace. If a namespace does not have the Citadel CA
		// certificate or the certificate in a ConfigMap of the namespace is not
		// up to date, Citadel updates the certificate in the namespace.
		// For simplifying the implementation and no overhead for reading the certificate from the ConfigMap,
		// simply updates the ConfigMap to the current Citadel CA certificate.
		err := certutil.InsertDataToConfigMapWithRetry(nc.core, ns.GetName(), string(rootCert), CACertNamespaceConfigMap,
			constants.CACertNamespaceConfigMapDataName, CACertNamespaceInsertInterval, CACertNamespaceInsertTimeout)
		if err != nil {
			log.Errorf("error when updating CA cert in configmap: %v", err)
		} else {
			log.Debugf("updated CA cert in configmap %v in ns %v",
				CACertNamespaceConfigMap, ns.GetName())
		}
	}
}
