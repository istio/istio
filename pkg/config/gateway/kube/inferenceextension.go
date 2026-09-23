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

package kube

// InferencePoolBackendConfig is what a single InferencePool backendRef contributes to a route
// rule: where to reach the pool's endpoint picker, and whether a picker failure fails open.
type InferencePoolBackendConfig struct {
	FQDN             string
	Port             string
	FailureModeAllow bool
}

// InferencePoolRouteRuleConfig holds the InferencePool backends of a single route rule, keyed by
// the hostname of the Service Istio synthesizes for each of them. A rule may weight traffic
// across several pools and each pool has its own endpoint picker, so this is resolved per
// backendRef rather than once per rule.
//
// This data is stored in the `Extra` field of a `config.Config` for a VirtualService, in a map
// keyed by the `HTTPRoute.Name`.
type InferencePoolRouteRuleConfig map[string]InferencePoolBackendConfig
