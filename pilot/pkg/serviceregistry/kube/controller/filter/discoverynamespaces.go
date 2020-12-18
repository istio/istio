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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	v1 "k8s.io/client-go/listers/core/v1"

	"istio.io/pkg/log"
)

const (
	// PilotDiscoveryLabelName is the name of the label used to indicate a namespace for discovery
	PilotDiscoveryLabelName = "istio-discovery"
	// PilotDiscoveryLabelValue is the value of for the label PilotDiscoveryLabelName used to indicate a namespace for discovery
	PilotDiscoveryLabelValue = "true"
)

func DiscoveryNamespacesFilterFunc(lister v1.NamespaceLister, enableDiscoveryNamespaces bool) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		// permit all objects if discovery namespaces is disabled
		if !enableDiscoveryNamespaces {
			return true
		}

		// filter out objects that don't reside in any discovery namespace
		selector := labels.Set(map[string]string{PilotDiscoveryLabelName: PilotDiscoveryLabelValue}).AsSelector()
		namespaceList, err := lister.List(selector)
		if err != nil {
			log.Errorf("failed to get namespaces: %v", err)
			return true
		}
		discoveryNamespaces := sets.NewString()
		for _, ns := range namespaceList {
			discoveryNamespaces.Insert(ns.Name)
		}

		return discoveryNamespaces.Has(obj.(metav1.Object).GetNamespace())
	}
}
