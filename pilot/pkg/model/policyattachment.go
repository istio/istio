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
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
)

type policyTargetGetter interface {
	GetTargetRef() *v1beta1.PolicyTargetReference
	GetSelector() *v1beta1.WorkloadSelector
}

type policyMatch string

const (
	policyMatchSelector policyMatch = "selector"
	policyMatchDirect   policyMatch = "direct"
	policyMatchIgnore   policyMatch = "ignore"
)

func getPolicyMatcher(policyName string, opts workloadSelectionOpts, policy policyTargetGetter) policyMatch {
	gatewayName, isGatewayAPI := opts.workloadLabels[constants.IstioGatewayLabel]
	targetRef := policy.GetTargetRef()
	if isGatewayAPI && targetRef == nil && policy.GetSelector() != nil {
		if opts.isWaypoint || features.EnableGatewayPolicyAttachmentOnly {
			log.Warnf("Ignoring workload-scoped RequestAuthentication %s.%s for gateway %s because it has no targetRef", opts.namespace, policyName, gatewayName)
			return policyMatchIgnore
		}
	}

	if !isGatewayAPI && targetRef != nil {
		return policyMatchIgnore
	}

	if isGatewayAPI && targetRef != nil {
		// There's a targetRef specified for this RA, and the proxy is a Gateway API Gateway. Use targetRef instead of workload selector
		// TODO: Account for `kind`s that are not `KubernetesGateway`
		if targetRef.GetGroup() == gvk.KubernetesGateway.Group &&
			targetRef.GetName() == gatewayName &&
			(targetRef.GetNamespace() == "" || targetRef.GetNamespace() == opts.namespace) &&
			targetRef.GetKind() == gvk.KubernetesGateway.Kind {
			return policyMatchDirect
		} else {
			// This config doesn't match this workload. Ignore
			return policyMatchIgnore
		}
	}

	// Default case
	return policyMatchSelector
}
