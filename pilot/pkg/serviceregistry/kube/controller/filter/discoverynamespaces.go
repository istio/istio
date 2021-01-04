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

package filter

import (
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/pkg/log"
)

const (
	// PilotDiscoveryLabelName is the name of the label used to indicate a namespace for discovery
	PilotDiscoveryLabelName = "istio-discovery"
	// PilotDiscoveryLabelValue is the value of for the label PilotDiscoveryLabelName used to indicate a namespace for discovery
	PilotDiscoveryLabelValue = "true"
)

var PilotDiscoverySelector = labels.Set(map[string]string{PilotDiscoveryLabelName: PilotDiscoveryLabelValue}).AsSelector()

// DiscoveryNamespacesFilter tracks the set of namespaces labeled for discovery, which are updated by an external namespace controller.
// It returns a filter function used for filtering out objects that don't reside in namespaces labeled for discovery.
type DiscoveryNamespacesFilter interface {
	GetFilter() func(obj interface{}) bool
	AddNamespace(ns string)
	RemoveNamespace(ns string)
}

type discoveryNamespacesFilter struct {
	lock                      sync.RWMutex
	enableDiscoveryNamespaces bool
	discoveryNamespaces       sets.String
}

func NewDiscoveryNamespacesFilter(enableDiscoveryNamespaces bool, nsLister listerv1.NamespaceLister) DiscoveryNamespacesFilter {
	discoveryNamespaces := sets.NewString()
	namespaceList, err := nsLister.List(PilotDiscoverySelector)

	if err != nil {
		log.Errorf("error initializing discovery namespaces filter, failed to list namespaces: %v", err)
	}

	for _, ns := range namespaceList {
		discoveryNamespaces.Insert(ns.Name)
	}

	return &discoveryNamespacesFilter{
		enableDiscoveryNamespaces: enableDiscoveryNamespaces,
		discoveryNamespaces:       discoveryNamespaces,
	}
}

func (d *discoveryNamespacesFilter) GetFilter() func(obj interface{}) bool {
	// permit all objects if discovery namespaces is disabled
	if !d.enableDiscoveryNamespaces {
		return func(_ interface{}) bool {
			return true
		}
	}

	// return true if object resides in a namespace labeled for discovery
	return func(obj interface{}) bool {
		d.lock.Lock()
		defer d.lock.Unlock()
		return d.discoveryNamespaces.Has(obj.(metav1.Object).GetNamespace())
	}
}

func (d *discoveryNamespacesFilter) AddNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Insert(ns)
}

func (d *discoveryNamespacesFilter) RemoveNamespace(ns string) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.discoveryNamespaces.Delete(ns)
}
