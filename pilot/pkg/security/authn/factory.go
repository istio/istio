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

package authn

import (
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

// NoOverride is an alias for MTLSUnknown to more clearly convey intent for InboundMTLSSettings
const NoOverride = model.MTLSUnknown

// PolicyApplier is the interface provides essential functionalities to help config Envoy (xDS) to enforce
// authentication policy. Each version of authentication policy will implement this interface.
type PolicyApplier interface {
	// InboundMTLSSettings returns inbound mTLS settings for a given workload port
	InboundMTLSSettings(endpointPort uint32, node *model.Proxy, trustDomainAliases []string, modeOverride model.MutualTLSMode) MTLSSettings

	// JwtFilter returns the JWT HTTP filter to enforce the underlying authentication policy.
	// It may return nil, if no JWT validation is needed.
	JwtFilter(clearRouteCache bool) *hcm.HttpFilter

	// PortLevelSetting returns port level mTLS settings.
	PortLevelSetting() map[uint32]model.MutualTLSMode

	MtlsPolicy
}

type MtlsPolicy interface {
	// GetMutualTLSModeForPort gets the mTLS mode for the given port. If there is no port level setting, it
	// returns the inherited namespace/mesh level setting.
	GetMutualTLSModeForPort(endpointPort uint32) model.MutualTLSMode
}

// NewPolicyApplier returns the appropriate (policy) applier, depends on the versions of the policy exists
// for the given service innstance.
func NewPolicyApplier(push *model.PushContext, proxy *model.Proxy, svc *model.Service) PolicyApplier {
	forWorkload := model.PolicyMatcherForProxy(proxy).WithService(svc)
	return newPolicyApplier(
		push.AuthnPolicies.GetRootNamespace(),
		push.AuthnPolicies.GetJwtPoliciesForWorkload(forWorkload),
		push.AuthnPolicies.GetPeerAuthenticationsForWorkload(forWorkload), push)
}

// NewMtlsPolicy returns a checker used to detect proxy mtls mode.
func NewMtlsPolicy(push *model.PushContext, namespace string, labels labels.Instance, isWaypoint bool) MtlsPolicy {
	return newPolicyApplier(
		push.AuthnPolicies.GetRootNamespace(),
		nil,
		push.AuthnPolicies.GetPeerAuthenticationsForWorkload(model.PolicyMatcherFor(namespace, labels, isWaypoint)),
		push,
	)
}
