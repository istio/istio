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

	"istio.io/istio/pkg/config/mesh"

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
	ConfigNamespaceConfigMap      = "istio-config"
	CACertNamespaceInsertInterval = time.Second
	CACertNamespaceInsertTimeout  = time.Second * 2
)

// NamespaceController manages Istio configmap in each namespace.
type NamespaceController struct {
	// ca will be used to write the CA cert
	ca *ca.IstioCA
	// mesh will be used to write the mesh config
	mesh mesh.Watcher
	core corev1.CoreV1Interface

	// Controller and store for namespace objects
	namespaceController cache.Controller
	namespaceStore      cache.Store
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(ca *ca.IstioCA, core corev1.CoreV1Interface, mesh mesh.Watcher) (*NamespaceController, error) {
	c := &NamespaceController{
		ca:   ca,
		core: core,
		mesh: mesh,
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

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	go nc.namespaceController.Run(stopCh)
	nc.mesh.AddMeshHandler(func() {
		log.Infof("Mesh config reloaded, updating all namespaces")
		nc.refresh()
	})
}

func (nc *NamespaceController) refresh() {
	for _, obj := range nc.namespaceStore.List() {
		ns, ok := obj.(*v1.Namespace)

		if ok {
			nc.updateConfigMap(ns)
		}
	}
}

// When a namespace is created, Citadel adds its public CA certificate
// to a well known configmap in the namespace.
func (nc *NamespaceController) namespaceAdded(obj interface{}) {
	ns, ok := obj.(*v1.Namespace)

	if ok {
		nc.updateConfigMap(ns)
	}
}

func (nc *NamespaceController) updateConfigMap(ns *v1.Namespace) {
	data := map[string]string{}
	if nc.ca != nil {
		data[constants.CACertNamespaceConfigMapDataName] = string(nc.ca.GetCAKeyCertBundle().GetRootCertPem())
	}
	m, err := mesh.MarshalMeshConfig(nc.mesh.Mesh())
	if err != nil {
		log.Errorf("failed to marshal mesh config: %v", err)
	} else {
		data[constants.MeshConfigNamespaceConfigMapDataName] = m
	}
	err = certutil.InsertDataToConfigMapWithRetry(nc.core, ns.GetName(), ConfigNamespaceConfigMap,
		data, CACertNamespaceInsertInterval, CACertNamespaceInsertTimeout)
	if err != nil {
		log.Errorf("error when inserting data to configmap: %v", err)
	} else {
		log.Debugf("inserted data to configmap %v in ns %v",
			ConfigNamespaceConfigMap, ns.GetName())
	}
}

func (nc *NamespaceController) namespaceUpdated(oldObj, newObj interface{}) {
	ns, ok := newObj.(*v1.Namespace)

	if ok {
		nc.updateConfigMap(ns)
	}
}
