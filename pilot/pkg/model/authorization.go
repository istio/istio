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

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	authzLog = istiolog.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

type AuthorizationPolicy struct {
	Name      string                      `json:"name"`
	Namespace string                      `json:"namespace"`
	Spec      *authpb.AuthorizationPolicy `json:"spec"`
}

// AuthorizationPolicies organizes AuthorizationPolicy by namespace.
type AuthorizationPolicies struct {
	// Maps from namespace to the Authorization policies.
	NamespaceToPolicies map[string][]AuthorizationPolicy `json:"namespace_to_policies"`

	// The name of the root namespace. Policy in the root namespace applies to workloads in all namespaces.
	RootNamespace string `json:"root_namespace"`
}

// GetAuthorizationPolicies returns the AuthorizationPolicies for the given environment.
func GetAuthorizationPolicies(env *Environment) (*AuthorizationPolicies, error) {
	policy := &AuthorizationPolicies{
		NamespaceToPolicies: map[string][]AuthorizationPolicy{},
		RootNamespace:       env.Mesh().GetRootNamespace(),
	}

	policies, err := env.List(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(policies)
	for _, config := range policies {
		authzConfig := AuthorizationPolicy{
			Name:      config.Name,
			Namespace: config.Namespace,
			Spec:      config.Spec.(*authpb.AuthorizationPolicy),
		}
		policy.NamespaceToPolicies[config.Namespace] =
			append(policy.NamespaceToPolicies[config.Namespace], authzConfig)
	}

	return policy, nil
}

// ListAuthorizationPolicies returns the deny and allow AuthorizationPolicy for the workload in the given namespace.
func (policy *AuthorizationPolicies) ListAuthorizationPolicies(namespace string, workload labels.Collection) (
	denyPolicies []AuthorizationPolicy, allowPolicies []AuthorizationPolicy) {
	if policy == nil {
		return
	}

	var namespaces []string
	if policy.RootNamespace != "" {
		namespaces = append(namespaces, policy.RootNamespace)
	}
	// To prevent duplicate policies in case root namespace equals proxy's namespace.
	if namespace != policy.RootNamespace {
		namespaces = append(namespaces, namespace)
	}

	for _, ns := range namespaces {
		for _, config := range policy.NamespaceToPolicies[ns] {
			spec := config.Spec
			selector := labels.Instance(spec.GetSelector().GetMatchLabels())
			if workload.IsSupersetOf(selector) {
				switch config.Spec.GetAction() {
				case authpb.AuthorizationPolicy_ALLOW:
					allowPolicies = append(allowPolicies, config)
				case authpb.AuthorizationPolicy_DENY:
					denyPolicies = append(denyPolicies, config)
				default:
					log.Errorf("ignored authorization policy %s.%s with unsupported action: %s",
						config.Namespace, config.Name, config.Spec.GetAction())
				}
			}
		}
	}

	return
}
