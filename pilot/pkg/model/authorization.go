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

package model

import (
	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
)

type AuthorizationPolicy struct {
	Name        string                      `json:"name"`
	Namespace   string                      `json:"namespace"`
	Annotations map[string]string           `json:"annotations"`
	Spec        *authpb.AuthorizationPolicy `json:"spec"`
}

// AuthorizationPolicies organizes AuthorizationPolicy by namespace.
type AuthorizationPolicies struct {
	// Maps from namespace to the Authorization policies.
	NamespaceToPolicies map[string][]AuthorizationPolicy `json:"namespace_to_policies"`

	// The name of the root namespace. Policy in the root namespace applies to workloads in all namespaces.
	RootNamespace string `json:"root_namespace"`
}

// GetAuthorizationPolicies returns the AuthorizationPolicies for the given environment.
func GetAuthorizationPolicies(env *Environment) *AuthorizationPolicies {
	policy := &AuthorizationPolicies{
		NamespaceToPolicies: map[string][]AuthorizationPolicy{},
		RootNamespace:       env.Mesh().GetRootNamespace(),
	}

	policies := env.List(gvk.AuthorizationPolicy, NamespaceAll)
	sortConfigByCreationTime(policies)
	for _, config := range policies {
		authzConfig := AuthorizationPolicy{
			Name:        config.Name,
			Namespace:   config.Namespace,
			Annotations: config.Annotations,
			Spec:        config.Spec.(*authpb.AuthorizationPolicy),
		}
		policy.NamespaceToPolicies[config.Namespace] = append(policy.NamespaceToPolicies[config.Namespace], authzConfig)
	}

	return policy
}

type AuthorizationPoliciesResult struct {
	Custom []AuthorizationPolicy
	Deny   []AuthorizationPolicy
	Allow  []AuthorizationPolicy
	Audit  []AuthorizationPolicy
}

type WorkloadSelectionOpts struct {
	Namespace    string
	WorkloadName string
	Workload     labels.Instance
}

// ListAuthorizationPolicies returns authorization policies applied to the workload in the given namespace.
func (policy *AuthorizationPolicies) ListAuthorizationPolicies(selectionInfo WorkloadSelectionOpts) AuthorizationPoliciesResult {
	ret := AuthorizationPoliciesResult{}
	if policy == nil {
		return ret
	}

	var namespaces []string
	if policy.RootNamespace != "" {
		namespaces = append(namespaces, policy.RootNamespace)
	}

	// Policies can be in root namespace and have workload selectors or a targetRef
	// This prevents duplicate policies in case root namespace equals proxy's namespace.
	if selectionInfo.Namespace != policy.RootNamespace {
		namespaces = append(namespaces, selectionInfo.Namespace)
	}

	for _, ns := range namespaces {
		for _, config := range policy.NamespaceToPolicies[ns] {
			spec := config.Spec
			targetRef := spec.GetTargetRef()

			// At this time, policies without a targetRef will default to workload selectors
			// TODO: remove this logic when workloadselector for waypoints is disallowed
			if targetRef == nil {
				selector := labels.Instance(spec.GetSelector().GetMatchLabels())
				if selector.SubsetOf(selectionInfo.Workload) {
					log.Infof("matching policy %s.%s does not have a targetRef", config.Namespace, config.Name)
					ret = updateAuthorizationPoliciesResult(config, ret)
				}
			} else if (targetRef.GetName() == selectionInfo.WorkloadName) && (ns == selectionInfo.Namespace) {
				log.Infof("matching policy %s.%s has a targetRef", config.Namespace, config.Name)
				ret = updateAuthorizationPoliciesResult(config, ret)
			}
		}
	}

	return ret
}

func updateAuthorizationPoliciesResult(config AuthorizationPolicy, ret AuthorizationPoliciesResult) AuthorizationPoliciesResult {
	log.Infof("applying authorization policy %s.%s",
		config.Namespace, config.Name)
	switch config.Spec.GetAction() {
	case authpb.AuthorizationPolicy_ALLOW:
		ret.Allow = append(ret.Allow, config)
	case authpb.AuthorizationPolicy_DENY:
		ret.Deny = append(ret.Deny, config)
	case authpb.AuthorizationPolicy_AUDIT:
		ret.Audit = append(ret.Audit, config)
	case authpb.AuthorizationPolicy_CUSTOM:
		ret.Custom = append(ret.Custom, config)
	default:
		log.Errorf("ignored authorization policy %s.%s with unsupported action: %s",
			config.Namespace, config.Name, config.Spec.GetAction())
	}
	return ret
}
