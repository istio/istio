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

func getWorkloadServiceEntries(ses []config.Config, wle *networking.WorkloadEntry) map[types.NamespacedName]*config.Config {
	out := make(map[types.NamespacedName]*config.Config)
	for i, cfg := range ses {
		se := cfg.Spec.(*networking.ServiceEntry)
		if se.WorkloadSelector != nil && labels.Instance(se.WorkloadSelector.Labels).Match(wle.Labels) {
			out[cfg.NamespacedName()] = &ses[i]
		}
	}

	return out
}

// returns a set of objects that are in `old` but not in `curr`
// For example:
// old = {a1, a2, a3}
// curr = {a1, a2, a4, a5}
// difference(old, curr) = {a3}
func difference(old, curr map[types.NamespacedName]*config.Config) []types.NamespacedName {
	var out []types.NamespacedName
	for key := range old {
		if _, ok := curr[key]; !ok {
			out = append(out, key)
		}
	}

	return out
}

func isDNSTypeServiceEntry(se *networking.ServiceEntry) bool {
	if se == nil {
		return false
	}
	return se.Resolution == networking.ServiceEntry_DNS || se.Resolution == networking.ServiceEntry_DNS_ROUND_ROBIN
}

// count the number of elements in the map value. The value is []any type.
func countSliceValue[M ~map[K][]V, K comparable, V any](m M) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}
