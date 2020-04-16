// Copyright 2018 Istio Authors
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
	"istio.io/istio/pkg/config/schema/collections"
	istiolog "istio.io/pkg/log"
)

var (
	authzLog = istiolog.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

type AuthorizationPolicyConfig struct {
	Name                string                      `json:"name"`
	Namespace           string                      `json:"namespace"`
	AuthorizationPolicy *authpb.AuthorizationPolicy `json:"authorization_policy"`
}

// AuthorizationPolicies organizes authorization policies by namespace.
// TODO(yangminzhu): Rename to avoid confusion from the AuthorizationPolicy CRD.
type AuthorizationPolicies struct {
	// Maps from namespace to the v1beta1 Authorization policies.
	NamespaceToV1beta1Policies map[string][]AuthorizationPolicyConfig `json:"namespace_to_v1beta1_policies"`

	// The name of the root namespace. Policy in the root namespace applies to workloads in all
	// namespaces. Only used for v1beta1 Authorization policy.
	RootNamespace string `json:"root_namespace"`
}

// GetAuthorizationPolicies gets the authorization policies in the mesh.
func GetAuthorizationPolicies(env *Environment) (*AuthorizationPolicies, error) {
	policy := &AuthorizationPolicies{
		NamespaceToV1beta1Policies: map[string][]AuthorizationPolicyConfig{},
		RootNamespace:              env.Mesh().GetRootNamespace(),
	}

	policies, err := env.List(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(policies)
	policy.addAuthorizationPolicies(policies)

	return policy, nil
}

// ListAuthorizationPolicies returns the AuthorizationPolicy for the workload in root namespace and the config namespace.
// The first one in the returned tuple is the deny policies and the second one is the allow policies.
func (policy *AuthorizationPolicies) ListAuthorizationPolicies(configNamespace string, workloadLabels labels.Collection) (
	denyPolicies []AuthorizationPolicyConfig, allowPolicies []AuthorizationPolicyConfig) {
	if policy == nil {
		return
	}

	var namespaces []string
	if policy.RootNamespace != "" {
		namespaces = append(namespaces, policy.RootNamespace)
	}
	// To prevent duplicate policies in case root namespace equals proxy's namespace.
	if configNamespace != policy.RootNamespace {
		namespaces = append(namespaces, configNamespace)
	}

	for _, ns := range namespaces {
		for _, config := range policy.NamespaceToV1beta1Policies[ns] {
			spec := config.AuthorizationPolicy
			selector := labels.Instance(spec.GetSelector().GetMatchLabels())
			if workloadLabels.IsSupersetOf(selector) {
				switch config.AuthorizationPolicy.GetAction() {
				case authpb.AuthorizationPolicy_ALLOW:
					allowPolicies = append(allowPolicies, config)
				case authpb.AuthorizationPolicy_DENY:
					denyPolicies = append(denyPolicies, config)
				default:
					log.Errorf("found authorization policy with unsupported action: %s", config.AuthorizationPolicy.GetAction())
				}
			}
		}
	}

	return
}

func (policy *AuthorizationPolicies) addAuthorizationPolicies(configs []Config) {
	if policy == nil {
		return
	}

	for _, config := range configs {
		authzConfig := AuthorizationPolicyConfig{
			Name:                config.Name,
			Namespace:           config.Namespace,
			AuthorizationPolicy: config.Spec.(*authpb.AuthorizationPolicy),
		}
		policy.NamespaceToV1beta1Policies[config.Namespace] =
			append(policy.NamespaceToV1beta1Policies[config.Namespace], authzConfig)
	}
}
