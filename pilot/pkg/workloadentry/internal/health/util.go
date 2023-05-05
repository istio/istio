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

package health

import (
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pkg/config"
)

// consider a workload elegible for health status updates as long as the
// WorkloadEntryHealthCheckAnnotation is present (no matter what the value is).
// In case the annotation is present but the value is not "true", the proxy should be allowed
// to send health status updates, config health condition should be updated accordingly,
// however reported health status should not come into effect.
func IsElegibleForHealthStatusUpdates(wle *config.Config) bool {
	if wle == nil {
		return false
	}
	_, annotated := wle.Annotations[status.WorkloadEntryHealthCheckAnnotation]
	return annotated
}
