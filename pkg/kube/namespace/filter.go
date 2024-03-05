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

package namespace

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

type DiscoveryFilter func(obj any) bool

type discoveryNamespacesFilter struct {
	lock                sync.RWMutex
	namespaces          kclient.Client[*corev1.Namespace]
	discoveryNamespaces sets.String
	discoverySelectors  []labels.Selector // nil if discovery selectors are not specified, permits all namespaces for discovery
	handlers            []func(added, removed sets.String)
}

func NewDiscoveryNamespacesFilter(
	namespaces kclient.Client[*corev1.Namespace],
	mesh mesh.Watcher,
	stop <-chan struct{},
) kubetypes.DynamicObjectFilter {
	// convert LabelSelectors to Selectors
	f := &discoveryNamespacesFilter{
		namespaces:          namespaces,
		discoveryNamespaces: sets.New[string](),
	}
	mesh.AddMeshHandler(func() {
		f.selectorsChanged(mesh.Mesh().GetDiscoverySelectors(), true)
	})

	namespaces.AddEventHandler(controllers.EventHandler[*corev1.Namespace]{
		AddFunc: func(ns *corev1.Namespace) {
			f.lock.Lock()
			defer f.lock.Unlock()
			// In rare cases, a namespace may be created after objects in the namespace, because there is no synchronization between watches
			// So we need to notify if we started selecting namespace
			if f.namespaceCreatedLocked(ns.ObjectMeta) {
				f.notifyHandlersLocked(sets.New(ns.Name), nil)
			}
		},
		UpdateFunc: func(old, new *corev1.Namespace) {
			f.lock.Lock()
			defer f.lock.Unlock()
			membershipChanged, namespaceAdded := f.namespaceUpdatedLocked(old.ObjectMeta, new.ObjectMeta)
			if membershipChanged {
				added := sets.New(new.Name)
				var removed sets.String
				if !namespaceAdded {
					removed = added
					added = nil
				}
				f.notifyHandlersLocked(added, removed)
			}
		},
		DeleteFunc: func(ns *corev1.Namespace) {
			// No need to notify handlers for deletes
			f.namespaceDeleted(ns.ObjectMeta)
		},
	})
	// Start namespaces and wait for it to be ready now. This is required for subsequent users, so we want to block
	namespaces.Start(stop)
	kube.WaitForCacheSync("discovery filter", stop, namespaces.HasSynced)
	f.selectorsChanged(mesh.Mesh().GetDiscoverySelectors(), false)
	return f
}

func (d *discoveryNamespacesFilter) notifyHandlersLocked(added sets.Set[string], removed sets.String) {
	for _, h := range d.handlers {
		h(added, removed)
	}
}

func (d *discoveryNamespacesFilter) Filter(obj any) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	// permit all objects if discovery selectors are not specified
	if len(d.discoverySelectors) == 0 {
		return true
	}

	if ns, ok := obj.(string); ok {
		return d.discoveryNamespaces.Contains(ns)
	}

	// When an object is deleted, obj could be a DeletionFinalStateUnknown marker item.
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	ns := object.GetNamespace()
	if _, ok := object.(*corev1.Namespace); ok {
		ns = object.GetName()
	}
	// permit if object resides in a namespace labeled for discovery
	return d.discoveryNamespaces.Contains(ns)
}

// SelectorsChanged initializes the discovery filter state with the discovery selectors and selected namespaces
func (d *discoveryNamespacesFilter) selectorsChanged(
	discoverySelectors []*metav1.LabelSelector,
	notify bool,
) {
	d.lock.Lock()
	defer d.lock.Unlock()
	var selectors []labels.Selector
	newDiscoveryNamespaces := sets.New[string]()

	namespaceList := d.namespaces.List("", labels.Everything())

	// convert LabelSelectors to Selectors
	for _, selector := range discoverySelectors {
		ls, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			log.Errorf("error initializing discovery namespaces filter, invalid discovery selector: %v", err)
			return
		}
		selectors = append(selectors, ls)
	}

	// range over all namespaces to get discovery namespaces
	for _, ns := range namespaceList {
		for _, selector := range selectors {
			if selector.Matches(labels.Set(ns.Labels)) {
				newDiscoveryNamespaces.Insert(ns.Name)
			}
		}
		// omitting discoverySelectors indicates discovering all namespaces
		if len(selectors) == 0 {
			for _, ns := range namespaceList {
				newDiscoveryNamespaces.Insert(ns.Name)
			}
		}
	}

	if notify {
		oldDiscoveryNamespaces := d.discoveryNamespaces
		selectedNamespaces := newDiscoveryNamespaces.Difference(oldDiscoveryNamespaces)
		deselectedNamespaces := oldDiscoveryNamespaces.Difference(newDiscoveryNamespaces)
		// Important: keep the lock while we call handlers. This allows handlers to ensure they do not miss events
		// if they are processing the change and new events come in.
		d.notifyHandlersLocked(selectedNamespaces, deselectedNamespaces)
	}
	// update filter state
	d.discoveryNamespaces = newDiscoveryNamespaces
	d.discoverySelectors = selectors
}

// namespaceCreated: if newly created namespace is selected, update namespace membership
func (d *discoveryNamespacesFilter) namespaceCreatedLocked(ns metav1.ObjectMeta) (membershipChanged bool) {
	if d.isSelectedLocked(ns.Labels) {
		d.discoveryNamespaces.Insert(ns.Name)
		// Do not trigger update when there are no selectors. This avoids possibility of double namespace ADDs
		return len(d.discoverySelectors) != 0
	}
	return false
}

// namespaceUpdatedLocked : if updated namespace was a member and no longer selected, or was not a member and now selected, update namespace membership
func (d *discoveryNamespacesFilter) namespaceUpdatedLocked(oldNs, newNs metav1.ObjectMeta) (membershipChanged bool, namespaceAdded bool) {
	if d.discoveryNamespaces.Contains(oldNs.Name) && !d.isSelectedLocked(newNs.Labels) {
		d.discoveryNamespaces.Delete(oldNs.Name)
		return true, false
	}
	if !d.discoveryNamespaces.Contains(oldNs.Name) && d.isSelectedLocked(newNs.Labels) {
		d.discoveryNamespaces.Insert(oldNs.Name)
		return true, true
	}
	return false, false
}

// namespaceDeleted : if deleted namespace was a member, remove it
func (d *discoveryNamespacesFilter) namespaceDeleted(ns metav1.ObjectMeta) (membershipChanged bool) {
	d.discoveryNamespaces.Delete(ns.Name)
	return d.isSelectedLocked(ns.Labels)
}

// AddHandler registers a handler on namespace, which will be triggered when namespace selected or deselected.
// If the namespaces have been synced, trigger the new added handler.
func (d *discoveryNamespacesFilter) AddHandler(f func(added, removed sets.String)) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.handlers = append(d.handlers, f)
}

func (d *discoveryNamespacesFilter) isSelectedLocked(labels labels.Set) bool {
	// permit all objects if discovery selectors are not specified
	if len(d.discoverySelectors) == 0 {
		return true
	}

	for _, selector := range d.discoverySelectors {
		if selector.Matches(labels) {
			return true
		}
	}

	return false
}
