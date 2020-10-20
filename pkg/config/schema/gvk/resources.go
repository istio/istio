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

package gvk

import (
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	Secret             = collections.K8SCoreV1Secrets.Resource().GroupVersionKind()
	GatewayClass       = collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind()
	ServiceApisGateway = collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind()
	HTTPRoute          = collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind()
	TCPRoute           = collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind()
	TLSRoute           = collections.K8SServiceApisV1Alpha1Tlsroutes.Resource().GroupVersionKind()
	BackendPolicy      = collections.K8SServiceApisV1Alpha1Backendpolicies.Resource().GroupVersionKind()
)
