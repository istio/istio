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
	"k8s.io/apimachinery/pkg/types"

	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
)

type AuthorizationPolicy struct {
	Name        string                      `json:"name"`
	Namespace   string                      `json:"namespace"`
	Annotations map[string]string           `json:"annotations"`
	Spec        *authpb.AuthorizationPolicy `json:"spec"`
}

func (ap *AuthorizationPolicy) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: ap.Name, Namespace: ap.Namespace}
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

	policyCount := make(map[string]int)
	for _, config := range policies {
		policyCount[config.Namespace]++
	}

	for _, config := range policies {
		authzConfig := AuthorizationPolicy{
			Name:        config.Name,
			Namespace:   config.Namespace,
			Annotations: config.Annotations,
			Spec:        config.Spec.(*authpb.AuthorizationPolicy),
		}
		if _, ok := policy.NamespaceToPolicies[config.Namespace]; !ok {
			policy.NamespaceToPolicies[config.Namespace] = make([]AuthorizationPolicy, 0, policyCount[config.Namespace])
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

// ListAuthorizationPolicies returns authorization policies applied to the workload in the given namespace.
func (policy *AuthorizationPolicies) ListAuthorizationPolicies(selectionOpts WorkloadPolicyMatcher) AuthorizationPoliciesResult {
	configs := AuthorizationPoliciesResult{}
	if policy == nil {
		return configs
	}

	if len(selectionOpts.Services) > 1 {
		// Currently, listing multiple services is unnecessary.
		// To simplify, this function allows at most one service.
		// The restriction can be lifted if future needs arise.
		panic("ListAuthorizationPolicies expects at most 1 service in WorkloadPolicyMatcher")
	}

	lookupInNamespaces := []string{policy.RootNamespace, selectionOpts.WorkloadNamespace}
	for _, svc := range selectionOpts.Services {
		lookupInNamespaces = append(lookupInNamespaces, svc.Namespace)
	}

	for _, ns := range slices.FilterDuplicates(lookupInNamespaces) {
		for _, config := range policy.NamespaceToPolicies[ns] {
			spec := config.Spec

			if selectionOpts.ShouldAttachPolicy(gvk.AuthorizationPolicy, config.NamespacedName(), spec) {
				configs = updateAuthorizationPoliciesResult(configs, config)
			}
		}
	}

	return configs
}

func updateAuthorizationPoliciesResult(configs AuthorizationPoliciesResult, config AuthorizationPolicy) AuthorizationPoliciesResult {
	log.Debugf("applying authorization policy %s.%s",
		config.Namespace, config.Name)
	switch config.Spec.GetAction() {
	case authpb.AuthorizationPolicy_ALLOW:
		configs.Allow = append(configs.Allow, config)
	case authpb.AuthorizationPolicy_DENY:
		configs.Deny = append(configs.Deny, config)
	case authpb.AuthorizationPolicy_AUDIT:
		configs.Audit = append(configs.Audit, config)
	case authpb.AuthorizationPolicy_CUSTOM:
		configs.Custom = append(configs.Custom, config)
	default:
		log.Errorf("ignored authorization policy %s.%s with unsupported action: %s",
			config.Namespace, config.Name, config.Spec.GetAction())
	}
	return configs
}
