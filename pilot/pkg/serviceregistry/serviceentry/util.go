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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
)

func getWorkloadServiceEntries(ses []config.Config, wle *networking.WorkloadEntry) map[types.NamespacedName]struct{} {
	workloadLabels := labels.Collection{wle.Labels}
	out := make(map[types.NamespacedName]struct{})
	for _, cfg := range ses {
		se := cfg.Spec.(*networking.ServiceEntry)

		if se.WorkloadSelector != nil && workloadLabels.IsSupersetOf(se.WorkloadSelector.Labels) {
			out[types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}] = struct{}{}
		}
	}

	return out
}

func compareServiceEntries(old, curr map[types.NamespacedName]struct{}) (selected, unSelected []types.NamespacedName) {
	for key := range curr {
		selected = append(selected, key)
	}
	for key := range old {
		if _, ok := curr[key]; !ok {
			unSelected = append(unSelected, key)
		}
	}

	return
}
