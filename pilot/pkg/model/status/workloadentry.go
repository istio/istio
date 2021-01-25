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

package status

const (
	// WorkloadEntryHealthCheckAnnotation is the annotation that is added to workload entries when
	// health checks are enabled.
	// If this annotation is present, a WorkloadEntry with the condition Healthy=False or Healthy not set
	// should be treated as unhealthy and not sent to proxies
	WorkloadEntryHealthCheckAnnotation = "proxy.istio.io/health-checks-enabled"

	// ConditionHealthy defines a status field to declare if a WorkloadEntry is healthy or not
	ConditionHealthy = "Healthy"
)
