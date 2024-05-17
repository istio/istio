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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

type DiscoveryFilter func(obj any) bool

// DiscoveryNamespacesFilter tracks the set of namespaces selected for discovery, which are updated by the discovery namespace controller.
// It exposes a filter function used for filtering out objects that don't reside in namespaces selected for discovery.
type DiscoveryNamespacesFilter interface {
	// Filter returns true if the input object or namespace string resides in a namespace selected for discovery
	Filter(obj any) bool
	// SelectorsChanged is invoked when meshConfig's discoverySelectors change
	SelectorsChanged(discoverySelectors []*metav1.LabelSelector)
	// GetMembers returns the namespaces selected for discovery
	GetMembers() sets.String
	// AddHandler registers a handler on namespace, which will be triggered when namespace selected or deselected.
	// If the namespaces have been synced, it will trigger the new added handler.
	AddHandler(func(ns string, event model.Event))
}

type discoveryNamespacesFilter struct {
	lock                sync.RWMutex
	namespaces          kclient.Client[*corev1.Namespace]
	discoveryNamespaces sets.String
	discoverySelectors  []labels.Selector // nil if discovery selectors are not specified, permits all namespaces for discovery
	handlers            []func(ns string, event model.Event)
}

func NewDiscoveryNamespacesFilter(
	namespaces kclient.Client[*corev1.Namespace],
	discoverySelectors []*metav1.LabelSelector,
) DiscoveryNamespacesFilter {
	// convert LabelSelectors to Selectors
	selectors := make([]labels.Selector, 0, len(discoverySelectors))
	for _, selector := range discoverySelectors {
		ls, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			log.Errorf("error initializing discovery namespaces filter, invalid discovery selector: %v", err)
		}
		selectors = append(selectors, ls)
	}
	f := &discoveryNamespacesFilter{
		namespaces:          namespaces,
		discoveryNamespaces: sets.New[string](),
		discoverySelectors:  selectors,
	}

	namespaces.AddEventHandler(controllers.EventHandler[*corev1.Namespace]{
		AddFunc: func(ns *corev1.Namespace) {
			if f.namespaceCreated(ns.ObjectMeta) {
				f.lock.RLock()
				defer f.lock.RUnlock()
				f.notifyNamespaceHandlers(ns.Name, model.EventAdd)
			}
		},
		UpdateFunc: func(old, new *corev1.Namespace) {
			membershipChanged, namespaceAdded := f.namespaceUpdated(old.ObjectMeta, new.ObjectMeta)
			if membershipChanged {
				if namespaceAdded {
					f.lock.RLock()
					defer f.lock.RUnlock()
					f.notifyNamespaceHandlers(new.Name, model.EventAdd)
				} else {
					f.lock.RLock()
					defer f.lock.RUnlock()
					f.notifyNamespaceHandlers(new.Name, model.EventDelete)
				}
			}
		},
		DeleteFunc: func(ns *corev1.Namespace) {
			f.lock.Lock()
			defer f.lock.Unlock()
			// no need to invoke object handlers since objects within the namespace will trigger delete events
			f.namespaceDeletedLocked(ns.ObjectMeta)
		},
	})

	return f
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
func (d *discoveryNamespacesFilter) SelectorsChanged(
	discoverySelectors []*metav1.LabelSelector,
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

	oldDiscoveryNamespaces := d.discoveryNamespaces
	selectedNamespaces := sets.SortedList(newDiscoveryNamespaces.Difference(oldDiscoveryNamespaces))
	deselectedNamespaces := sets.SortedList(oldDiscoveryNamespaces.Difference(newDiscoveryNamespaces))
	for _, ns := range selectedNamespaces {
		d.notifyNamespaceHandlers(ns, model.EventAdd)
	}
	for _, ns := range deselectedNamespaces {
		d.notifyNamespaceHandlers(ns, model.EventDelete)
	}
	// update filter state
	d.discoveryNamespaces = newDiscoveryNamespaces
	d.discoverySelectors = selectors
}

func (d *discoveryNamespacesFilter) notifyNamespaceHandlers(ns string, event model.Event) {
	for _, h := range d.handlers {
		h(ns, event)
	}
}

// namespaceCreated : if newly created namespace is selected, update namespace membership
func (d *discoveryNamespacesFilter) namespaceCreated(ns metav1.ObjectMeta) (membershipChanged bool) {
	if d.isSelected(ns.Labels) {
		d.addNamespace(ns.Name)
		return true
	}
	return false
}

// namespaceUpdated : if updated namespace was a member and no longer selected, or was not a member and now selected, update namespace membership
func (d *discoveryNamespacesFilter) namespaceUpdated(oldNs, newNs metav1.ObjectMeta) (membershipChanged bool, namespaceAdded bool) {
	if d.hasNamespace(oldNs.Name) && !d.isSelected(newNs.Labels) {
		d.removeNamespace(oldNs.Name)
		return true, false
	}
	if !d.hasNamespace(oldNs.Name) && d.isSelected(newNs.Labels) {
		d.addNamespace(oldNs.Name)
		return true, true
	}
	return false, false
}

// namespaceDeletedLocked : if deleted namespace was a member, remove it
func (d *discoveryNamespacesFilter) namespaceDeletedLocked(ns metav1.ObjectMeta) {
	d.discoveryNamespaces.Delete(ns.Name)
}

// GetMembers returns member namespaces
func (d *discoveryNamespacesFilter) GetMembers() sets.String {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.discoveryNamespaces.Copy()
}

// AddHandler registers a handler on namespace, which will be triggered when namespace selected or deselected.
// If the namespaces have been synced, trigger the new added handler.
func (d *discoveryNamespacesFilter) AddHandler(f func(ns string, event model.Event)) {
	d.lock.Lock()
	defer d.lock.Unlock()
	for ns := range d.discoveryNamespaces {
		f(ns, model.EventAdd)
	}
	d.handlers = append(d.handlers, f)
}

func (d *discoveryNamespacesFilter) addNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Insert(ns)
}

func (d *discoveryNamespacesFilter) hasNamespace(ns string) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.discoveryNamespaces.Contains(ns)
}

func (d *discoveryNamespacesFilter) removeNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Delete(ns)
}

func (d *discoveryNamespacesFilter) isSelected(labels labels.Set) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
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
