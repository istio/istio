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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/security/pkg/k8s"
)

const (
	// maxRetries is the number of times a namespace will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuing of a namespace.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms
	maxRetries = 5
)

var (
	// CACertNamespaceConfigMap is the name of the ConfigMap in each namespace storing the root cert of non-Kube CA.
	CACertNamespaceConfigMap = features.CACertConfigMapName

	configMapLabel = map[string]string{"istio.io/config": "true"}
)

// NamespaceController manages reconciles a configmap in each namespace with a desired set of data.
type NamespaceController struct {
	caBundleWatcher *keycertbundle.Watcher

	queue controllers.Queue

	namespaces kclient.Client[*v1.Namespace]
	configmaps kclient.Client[*v1.ConfigMap]

	ignoredNamespaces sets.Set[string]
}

// NewNamespaceController returns a pointer to a newly constructed NamespaceController instance.
func NewNamespaceController(kubeClient kube.Client, caBundleWatcher *keycertbundle.Watcher) *NamespaceController {
	c := &NamespaceController{
		caBundleWatcher: caBundleWatcher,
	}
	c.queue = controllers.NewQueue("namespace controller",
		controllers.WithReconciler(c.reconcileCACert),
		controllers.WithMaxAttempts(maxRetries))

	c.configmaps = kclient.NewFiltered[*v1.ConfigMap](kubeClient, kclient.Filter{
		FieldSelector: "metadata.name=" + CACertNamespaceConfigMap,
		ObjectFilter:  kubeClient.ObjectFilter(),
	})
	c.namespaces = kclient.NewFiltered[*v1.Namespace](kubeClient, kclient.Filter{
		ObjectFilter: kubeClient.ObjectFilter(),
	})
	// kube-system is not skipped to enable deploying ztunnel in that namespace
	c.ignoredNamespaces = inject.IgnoredNamespaces.Copy().Delete(constants.KubeSystemNamespace)

	c.configmaps.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		// skip special kubernetes system namespaces
		return !c.ignoredNamespaces.Contains(o.GetNamespace())
	}))

	c.namespaces.AddEventHandler(controllers.FilteredObjectSpecHandler(c.queue.AddObject, func(o controllers.Object) bool {
		if features.InformerWatchNamespace != "" && features.InformerWatchNamespace != o.GetName() {
			// We are only watching one namespace, and its not this one
			return false
		}
		if c.ignoredNamespaces.Contains(o.GetName()) {
			// skip special kubernetes system namespaces
			return false
		}
		return true
	}))
	return c
}

// Run starts the NamespaceController until a value is sent to stopCh.
func (nc *NamespaceController) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("namespace controller", stopCh, nc.namespaces.HasSynced, nc.configmaps.HasSynced) {
		nc.queue.ShutDownEarly()
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

	meta := metav1.ObjectMeta{
		Name:      CACertNamespaceConfigMap,
		Namespace: ns,
		Labels:    configMapLabel,
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
	if nc.ignoredNamespaces.Contains(ns) {
		return
	}
	nc.queue.Add(types.NamespacedName{Name: ns})
}
