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

package workloadinstances

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

// ByServiceSelector returns a predicate that matches workload instances
// of a given service.
func ByServiceSelector(namespace string, selector labels.Collection) func(*model.WorkloadInstance) bool {
	return func(wi *model.WorkloadInstance) bool {
		return wi.Namespace == namespace && selector.HasSubsetOf(wi.Endpoint.Labels)
	}
}

// FindAllInIndex returns a list of workload instances in the index
// that match given predicate.
//
// The returned list is not ordered.
func FindAllInIndex(index Index, predicate func(*model.WorkloadInstance) bool) []*model.WorkloadInstance {
	var instances []*model.WorkloadInstance
	index.ForEach(func(instance *model.WorkloadInstance) {
		if predicate(instance) {
			instances = append(instances, instance)
		}
	})
	return instances
}
