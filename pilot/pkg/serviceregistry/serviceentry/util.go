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

package serviceentry

import (
	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/labels"
)

func getWorkloadServiceEntries(ses []servicesWithEntry, wle *networking.WorkloadEntry) map[types.NamespacedName]struct{} {
	workloadLabels := labels.Collection{wle.Labels}
	out := make(map[types.NamespacedName]struct{})
	for _, se := range ses {
		if workloadLabels.IsSupersetOf(se.entry.WorkloadSelector.Labels) {
			out[se.key] = struct{}{}
		}
	}

	return out
}

func compareServiceEntries(old, curr map[types.NamespacedName]struct{}) (newSelected, deSelected, unchanged map[types.NamespacedName]struct{}) {
	newSelected = make(map[types.NamespacedName]struct{})
	deSelected = make(map[types.NamespacedName]struct{})
	unchanged = make(map[types.NamespacedName]struct{})
	for key := range curr {
		if _, ok := old[key]; !ok {
			newSelected[key] = struct{}{}
		} else {
			unchanged[key] = struct{}{}
		}
	}

	for key := range old {
		if _, ok := curr[key]; !ok {
			deSelected[key] = struct{}{}
		}
	}

	return newSelected, deSelected, unchanged
}
