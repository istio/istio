// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
)

type PolicyTargetGetter interface {
	GetTargetRef() *v1beta1.PolicyTargetReference
	GetTargetRefs() []*v1beta1.PolicyTargetReference
	GetSelector() *v1beta1.WorkloadSelector
}

// GetTargetRefs returns the list of targetRefs, taking into account the legacy targetRef
func GetTargetRefs(p PolicyTargetGetter) []*v1beta1.PolicyTargetReference {
	targetRefs := p.GetTargetRefs()
	if len(targetRefs) == 0 && p.GetTargetRef() != nil {
		targetRefs = []*v1beta1.PolicyTargetReference{p.GetTargetRef()}
	}
	return targetRefs
}

type WorkloadSelectionOpts struct {
	RootNamespace  string
	Namespace      string
	WorkloadLabels labels.Instance
	IsWaypoint     bool
}

type policyMatch string

const (
	// policyMatchSelector is the default behavior. If the workload matches the policy's selector, the policy is applied
	policyMatchSelector policyMatch = "selector"
	// policyMatchDirect is used when the policy has a targetRef, and the workload matches the targetRef.
	// Note that the actual targetRef matching is done within `getPolicyMatcher`
	policyMatchDirect policyMatch = "direct"
	// policyMatchIgnore indicates that there is no match between the workload and the policy, and the policy should be ignored
	policyMatchIgnore policyMatch = "ignore"
)

func KubernetesGatewayNameAndExists(l labels.Instance) (string, bool) {
	gwName, exists := l[constants.GatewayNameLabel]
	if !exists {
		// TODO: Remove deprecated gateway name label (1.22 or 1.23)
		gwName, exists = l[constants.DeprecatedGatewayNameLabel]
	}

	return gwName, exists
}

func getPolicyMatcher(kind config.GroupVersionKind, policyName string, opts WorkloadSelectionOpts, policy PolicyTargetGetter) policyMatch {
	gatewayName, isGatewayAPI := KubernetesGatewayNameAndExists(opts.WorkloadLabels)
	targetRefs := GetTargetRefs(policy)
	if isGatewayAPI && len(targetRefs) == 0 && policy.GetSelector() != nil {
		if opts.IsWaypoint || !features.EnableSelectorBasedK8sGatewayPolicy {
			log.Debugf("Ignoring workload-scoped %s/%s %s.%s for gateway %s because it has no targetRef", kind.Group, kind.Kind, opts.Namespace, policyName, gatewayName)
			return policyMatchIgnore
		}
	}

	if !isGatewayAPI && len(targetRefs) > 0 {
		return policyMatchIgnore
	}

	if isGatewayAPI && len(targetRefs) > 0 {
		// There's a targetRef specified for this policy, and the proxy is a Gateway API Gateway. Use targetRef instead of workload selector
		// TODO: Account for `kind`s that are not `KubernetesGateway`
		for _, targetRef := range targetRefs {
			if targetRef.GetGroup() == gvk.KubernetesGateway.Group &&
				targetRef.GetName() == gatewayName &&
				(targetRef.GetNamespace() == "" || targetRef.GetNamespace() == opts.Namespace) &&
				targetRef.GetKind() == gvk.KubernetesGateway.Kind {
				return policyMatchDirect
			}
		}

		// This config doesn't match this workload. Ignore
		return policyMatchIgnore
	}

	// Default case
	return policyMatchSelector
}
