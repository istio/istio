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

package loadbalancer

import (
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/slices"
)

// DynamicSubsetKeys returns every key referenced by the dynamic subset settings, in a stable order.
// These are the keys endpoints must expose as metadata for the subsets to be computed.
func DynamicSubsetKeys(ds *networking.DynamicSubsetLB) []string {
	if ds == nil {
		return nil
	}
	var keys []string
	for _, s := range ds.GetSubsetSelectors() {
		for _, k := range s.GetKeys() {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	for k := range ds.GetDefaultSubset() {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return slices.Sort(keys)
}
