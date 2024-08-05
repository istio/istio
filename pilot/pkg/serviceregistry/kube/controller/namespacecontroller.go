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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/ali/config/ownerreference"
	alifeatures "istio.io/istio/pkg/ali/features"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/security/pkg/k8s"
)

const (
	// CACertNamespaceConfigMap is the name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap = "istio-ca-root-cert"

	// maxRetries is the number of times a namespace will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuing of a namespace.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms
	maxRetries = 5
)

var (
	configMapLabel = map[string]string{"istio.io/config": "true"}
	// Add by ingress
	dynamicCACertNamespaceConfigMap = CACertNamespaceConfigMap
)

// Add by ingress
func init() {
	if features.ClusterName != "" && features.ClusterName != "Kubernetes" {
		dynamicCACertNamespaceConfigMap = fmt.Sprintf("%s-ca-root-cert", features.ClusterName)
	}
}

// End add by ingress

// NamespaceController manages reconciles a configmap in each namespace with a desired set of data.
type NamespaceController struct {
	caBundleWatcher *keycertbundle.Watcher

	queue controllers.Queue

	namespaces kclient.Client[*v1.Namespace]
	configmaps kclient.Client[*v1.ConfigMap]

	// if meshConfig.DiscoverySelectors specified, DiscoveryNamespacesFilter tracks the namespaces to be watched by this controller.
	DiscoveryNamespacesFilter namespace.DiscoveryNamespacesFilter
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(kubeClient kube.Client, caBundleWatcher *keycertbundle.Watcher,
	discoveryNamespacesFilter namespace.DiscoveryNamespacesFilter,
) *NamespaceController {
	c := &NamespaceController{
		caBundleWatcher:           caBundleWatcher,
		DiscoveryNamespacesFilter: discoveryNamespacesFilter,
	}
	c.queue = controllers.NewQueue("namespace controller",
		controllers.WithReconciler(c.reconcileCACert),
		controllers.WithMaxAttempts(maxRetries))

	// updated by ingress
	if alifeatures.WatchResourcesByNamespaceForPrimaryCluster != "" {
		c.configmaps = kclient.NewFiltered[*v1.ConfigMap](kubeClient, kclient.Filter{
			FieldSelector: "metadata.name=" + dynamicCACertNamespaceConfigMap,
			ObjectFilter:  c.GetFilter(),
		})
	} else {
		c.configmaps = kclient.NewFiltered[*v1.ConfigMap](kubeClient, kclient.Filter{
			FieldSelector: "metadata.name=" + CACertNamespaceConfigMap,
			ObjectFilter:  c.GetFilter(),
		})
	}

	c.namespaces = kclient.New[*v1.Namespace](kubeClient)

	c.configmaps.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		// Add by ingress
		if o.GetName() != dynamicCACertNamespaceConfigMap {
			// This is a change to a configmap we don't watch, ignore it
			return false
		}
		// End add by ingress
		// skip special kubernetes system namespaces
		return !inject.IgnoredNamespaces.Contains(o.GetNamespace())
	}))

	if c.DiscoveryNamespacesFilter != nil {
		c.DiscoveryNamespacesFilter.AddHandler(func(ns string, event model.Event) {
			c.syncNamespace(ns)
		})
	} else {
		c.namespaces.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
			// Add by ingress
			if alifeatures.WatchResourcesByNamespaceForPrimaryCluster != "" {
				if o.GetName() != alifeatures.WatchResourcesByNamespaceForPrimaryCluster {
					// This is a change to a namespace we don't watch, ignore it
					return false
				}
			}
			// End add by ingress
			if features.InformerWatchNamespace != "" && features.InformerWatchNamespace != o.GetName() {
				// We are only watching one namespace, and its not this one
				return false
			}
			if inject.IgnoredNamespaces.Contains(o.GetName()) {
				// skip special kubernetes system namespaces
				return false
			}
			return true
		}))
	}
	return c
}

func (nc *NamespaceController) GetFilter() namespace.DiscoveryFilter {
	if nc.DiscoveryNamespacesFilter != nil {
		return nc.DiscoveryNamespacesFilter.Filter
	}
	return nil
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("namespace controller", stopCh, nc.namespaces.HasSynced, nc.configmaps.HasSynced) {
		return
	}

	go nc.startCaBundleWatcher(stopCh)
	nc.queue.Run(stopCh)
	controllers.ShutdownAll(nc.configmaps, nc.namespaces)
}

// startCaBundleWatcher listens for updates to the CA bundle and update cm in each namespace
func (nc *NamespaceController) startCaBundleWatcher(stop <-chan struct{}) {
	id, watchCh := nc.caBundleWatcher.AddWatcher()
	defer nc.caBundleWatcher.RemoveWatcher(id)
	for {
		select {
		case <-watchCh:
			for _, ns := range nc.namespaces.List("", labels.Everything()) {
				nc.namespaceChange(ns)
			}
		case <-stop:
			return
		}
	}
}

// reconcileCACert will reconcile the ca root cert configmap for the specified namespace
// If the configmap is not found, it will be created.
// If the namespace is filtered out by discovery selector, the configmap will be deleted.
func (nc *NamespaceController) reconcileCACert(o types.NamespacedName) error {
	ns := o.Namespace
	if ns == "" {
		// For Namespace object, it will not have o.Namespace field set
		ns = o.Name
	}
	if nc.DiscoveryNamespacesFilter != nil && !nc.DiscoveryNamespacesFilter.Filter(ns) {
		// do not delete the configmap, maybe it is owned by another control plane
		return nil
	}

	meta := metav1.ObjectMeta{
		Name:            dynamicCACertNamespaceConfigMap,
		Namespace:       ns,
		Labels:          configMapLabel,
		OwnerReferences: []metav1.OwnerReference{ownerreference.GenOwnerReference()},
	}
	return k8s.InsertDataToConfigMap(nc.configmaps, meta, nc.caBundleWatcher.GetCABundle())
}

// On namespace change, update the config map.
// If terminating, this will be skipped
func (nc *NamespaceController) namespaceChange(ns *v1.Namespace) {
	if ns.Status.Phase != v1.NamespaceTerminating {
		nc.syncNamespace(ns.Name)
	}
}

func (nc *NamespaceController) syncNamespace(ns string) {
	// skip special kubernetes system namespaces
	if inject.IgnoredNamespaces.Contains(ns) {
		return
	}

	// Add by ingress
	if alifeatures.WatchResourcesByNamespaceForPrimaryCluster != "" {
		if ns != alifeatures.WatchResourcesByNamespaceForPrimaryCluster {
			// This is a change to a namespace we don't watch, ignore it
			return
		}
	}
	// End add by ingress

	nc.queue.Add(types.NamespacedName{Name: ns})
}
