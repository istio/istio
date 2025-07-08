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

// InferencePoolRouteRuleConfig holds configuration for an inference pool associated with a route.
// This data is stored in the `Extra` field of a `config.Config` for a VirtualService.
// The map is keyed by the `HTTPRoute.Name`.
type InferencePoolRouteRuleConfig struct {
	FQDN             string
	Port             string
	FailureModeAllow bool
}
