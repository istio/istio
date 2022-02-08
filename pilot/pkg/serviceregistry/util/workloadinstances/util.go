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
)

// FindInstance returns the first workload instance matching given predicate.
func FindInstance(instances []*model.WorkloadInstance, predicate func(*model.WorkloadInstance) bool) *model.WorkloadInstance {
	for _, instance := range instances {
		if predicate(instance) {
			return instance
		}
	}
	return nil
}
