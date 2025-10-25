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
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
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
	reg := mesh.AddMeshHandler(func() {
		f.selectorsChanged(mesh.Mesh().GetDiscoverySelectors(), true)
	})

	// Clean up mesh handler on stop
	go func() {
		<-stop
		reg.Remove()
	}()

	namespaces.AddEventHandler(controllers.EventHandler[*corev1.Namespace]{
		AddFunc: func(ns *corev1.Namespace) {
			f.lock.Lock()
			created := f.namespaceCreatedLocked(ns.ObjectMeta)
			f.lock.Unlock()
			// In rare cases, a namespace may be created after objects in the namespace, because there is no synchronization between watches
			// So we need to notify if we started selecting namespace
			if created {
				f.notifyHandlers(sets.New(ns.Name), nil)
			}
		},
		UpdateFunc: func(oldObj, newObj *corev1.Namespace) {
			f.lock.Lock()
			membershipChanged, namespaceAdded := f.namespaceUpdatedLocked(oldObj.ObjectMeta, newObj.ObjectMeta)
			f.lock.Unlock()
			if membershipChanged {
				added := sets.New(newObj.Name)
				var removed sets.String
				if !namespaceAdded {
					removed = added
					added = nil
				}
				f.notifyHandlers(added, removed)
			}
		},
		DeleteFunc: func(ns *corev1.Namespace) {
			f.lock.Lock()
			defer f.lock.Unlock()
			// No need to notify handlers for deletes. The namespace was deleted, so the object will be as well (and a delete could not de-select).
			// Note that specifically for the edge case of a Namespace watcher that is filtering, this will ignore deletes we should
			// otherwise send.
			// See kclient.applyDynamicFilter for rationale.
			f.namespaceDeletedLocked(ns.ObjectMeta)
		},
	})
	// Start namespaces and wait for it to be ready now. This is required for subsequent users, so we want to block
	namespaces.Start(stop)
	kube.WaitForCacheSync("discovery filter", stop, namespaces.HasSynced)
	f.selectorsChanged(mesh.Mesh().GetDiscoverySelectors(), false)
	return f
}

func (d *discoveryNamespacesFilter) notifyHandlers(added sets.Set[string], removed sets.String) {
	// Clone handlers; we handle dynamic handlers so they can change after the filter has started.
	// Important: handlers are not called under the lock. If they are, then handlers which eventually call discoveryNamespacesFilter.Filter
	// (as some do in the codebase currently, via kclient.List), will deadlock.
	d.lock.RLock()
	handlers := slices.Clone(d.handlers)
	d.lock.RUnlock()
	for _, h := range handlers {
		h(added, removed)
	}
}

func (d *discoveryNamespacesFilter) Filter(obj any) bool {
	// When an object is deleted, obj could be a DeletionFinalStateUnknown marker item.
	ns, ok := extractObjectNamespace(obj)
	if !ok {
		return false
	}
	if ns == "" {
		// Cluster scoped resources. Always included
		return true
	}

	d.lock.RLock()
	defer d.lock.RUnlock()
	// permit all objects if discovery selectors are not specified
	if len(d.discoverySelectors) == 0 {
		return true
	}

	// permit if object resides in a namespace labeled for discovery
	return d.discoveryNamespaces.Contains(ns)
}

func extractObjectNamespace(obj any) (string, bool) {
	if ns, ok := obj.(string); ok {
		return ns, true
	}
	object := controllers.ExtractObject(obj)
	if object == nil {
		// When an object is deleted, obj could be a DeletionFinalStateUnknown marker item.
		return "", false
	}
	if _, ok := object.(*corev1.Namespace); ok {
		return object.GetName(), true
	}
	return object.GetNamespace(), true
}

func LabelSelectorAsSelector(ps *meshapi.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}
	requirements := make([]labels.Requirement, 0, len(ps.MatchLabels)+len(ps.MatchExpressions))
	for k, v := range ps.MatchLabels {
		r, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch metav1.LabelSelectorOperator(expr.Operator) {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid label selector operator", expr.Operator)
		}
		r, err := labels.NewRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, *r)
	}
	selector := labels.NewSelector()
	selector = selector.Add(requirements...)
	return selector, nil
}

// SelectorsChanged initializes the discovery filter state with the discovery selectors and selected namespaces
func (d *discoveryNamespacesFilter) selectorsChanged(
	discoverySelectors []*meshapi.LabelSelector,
	notify bool,
) {
	// Call closure to allow safe defer lock handling
	selectedNamespaces, deselectedNamespaces := func() (sets.String, sets.String) {
		d.lock.Lock()
		defer d.lock.Unlock()
		var selectors []labels.Selector
		newDiscoveryNamespaces := sets.New[string]()

		namespaceList := d.namespaces.List("", labels.Everything())

		// convert LabelSelectors to Selectors
		for _, selector := range discoverySelectors {
			ls, err := LabelSelectorAsSelector(selector)
			if err != nil {
				log.Errorf("error initializing discovery namespaces filter, invalid discovery selector: %v", err)
				return nil, nil
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

		// update filter state
		oldDiscoveryNamespaces := d.discoveryNamespaces
		d.discoveryNamespaces = newDiscoveryNamespaces
		d.discoverySelectors = selectors
		if notify {
			selectedNamespaces := newDiscoveryNamespaces.Difference(oldDiscoveryNamespaces)
			deselectedNamespaces := oldDiscoveryNamespaces.Difference(newDiscoveryNamespaces)
			return selectedNamespaces, deselectedNamespaces
		}
		return nil, nil
	}()
	if notify {
		d.notifyHandlers(selectedNamespaces, deselectedNamespaces)
	}
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

// namespaceDeletedLocked : if deleted namespace was a member, remove it
func (d *discoveryNamespacesFilter) namespaceDeletedLocked(ns metav1.ObjectMeta) {
	d.discoveryNamespaces.Delete(ns.Name)
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
