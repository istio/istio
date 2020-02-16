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
	rbacproto "istio.io/api/rbac/v1alpha1"
	authpb "istio.io/api/security/v1beta1"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type ServiceRoleConfig struct {
	Name        string                 `json:"name"`
	ServiceRole *rbacproto.ServiceRole `json:"service_role"`
}

type AuthorizationPolicyConfig struct {
	Name                string                      `json:"name"`
	Namespace           string                      `json:"namespace"`
	AuthorizationPolicy *authpb.AuthorizationPolicy `json:"authorization_policy"`
}

// RolesAndBindings stores the the ServiceRole and ServiceRoleBinding in the same namespace.
type RolesAndBindings struct {
	// ServiceRoles in the same namespace.
	Roles []ServiceRoleConfig `json:"roles"`

	// ServiceRoleBindings indexed by its associated ServiceRole's name.
	Bindings map[string][]*rbacproto.ServiceRoleBinding `json:"bindings"`
}

// AuthorizationPolicies organizes authorization policies by namespace.
// TODO(yangminzhu): Rename to avoid confusion from the AuthorizationPolicy CRD.
type AuthorizationPolicies struct {
	// Maps from namespace to the v1alpha1 RBAC policies, deprecated by v1beta1 Authorization policy.
	NamespaceToV1alpha1Policies map[string]*RolesAndBindings `json:"namespace_to_v1alpha1_policies"`

	// The mesh global RbacConfig, deprecated by v1beta1 Authorization policy.
	RbacConfig *rbacproto.RbacConfig `json:"rbac_config"`

	// Maps from namespace to the v1beta1 Authorization policies.
	NamespaceToV1beta1Policies map[string][]AuthorizationPolicyConfig `json:"namespace_to_v1beta1_policies"`

	// The name of the root namespace. Policy in the root namespace applies to workloads in all
	// namespaces. Only used for v1beta1 Authorization policy.
	RootNamespace string `json:"root_namespace"`
}

// GetAuthorizationPolicies gets the authorization policies in the mesh.
func GetAuthorizationPolicies(env *Environment) (*AuthorizationPolicies, error) {
	policy := &AuthorizationPolicies{
		NamespaceToV1alpha1Policies: map[string]*RolesAndBindings{},
		NamespaceToV1beta1Policies:  map[string][]AuthorizationPolicyConfig{},
		RootNamespace:               env.Mesh().GetRootNamespace(),
	}

	rbacConfig := env.IstioConfigStore.ClusterRbacConfig()
	if rbacConfig == nil {
		rbacConfig = env.IstioConfigStore.RbacConfig()
	}
	if rbacConfig != nil {
		policy.RbacConfig = rbacConfig.Spec.(*rbacproto.RbacConfig)
	}

	roles, err := env.List(collections.IstioRbacV1Alpha1Serviceroles.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(roles)
	policy.addServiceRoles(roles)

	bindings, err := env.List(collections.IstioRbacV1Alpha1Servicerolebindings.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(bindings)
	policy.addServiceRoleBindings(bindings)

	policies, err := env.List(collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(policies)
	policy.addAuthorizationPolicies(policies)

	return policy, nil
}

// GetClusterRbacConfig returns the global RBAC config.
func (policy *AuthorizationPolicies) GetClusterRbacConfig() *rbacproto.RbacConfig {
	return policy.RbacConfig
}

// IsRBACEnabled returns true if RBAC is enabled for the service in the given namespace.
func (policy *AuthorizationPolicies) IsRBACEnabled(service string, namespace string) bool {
	if policy == nil || policy.RbacConfig == nil {
		return false
	}

	// If service or namespace is empty just return false.
	if len(service) == 0 || len(namespace) == 0 {
		return false
	}

	rbacConfig := policy.RbacConfig
	switch rbacConfig.Mode {
	case rbacproto.RbacConfig_ON:
		return true
	case rbacproto.RbacConfig_ON_WITH_INCLUSION:
		return isInRbacTargetList(service, namespace, rbacConfig.Inclusion)
	case rbacproto.RbacConfig_ON_WITH_EXCLUSION:
		return !isInRbacTargetList(service, namespace, rbacConfig.Exclusion)
	default:
		return false
	}
}

// IsGlobalPermissiveEnabled returns true if global permissive mode is enabled.
func (policy *AuthorizationPolicies) IsGlobalPermissiveEnabled() bool {
	return policy != nil && policy.RbacConfig != nil &&
		policy.RbacConfig.EnforcementMode == rbacproto.EnforcementMode_PERMISSIVE
}

// ListV1alpha1Namespaces returns all namespaces that have V1alpha1 policies.
func (policy *AuthorizationPolicies) ListV1alpha1Namespaces() []string {
	if policy == nil {
		return nil
	}
	namespaces := make([]string, 0, len(policy.NamespaceToV1alpha1Policies))
	for ns := range policy.NamespaceToV1alpha1Policies {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// ListServiceRoles returns ServiceRole in the given namespace.
func (policy *AuthorizationPolicies) ListServiceRoles(ns string) []ServiceRoleConfig {
	if policy == nil {
		return nil
	}

	rolesAndBindings := policy.NamespaceToV1alpha1Policies[ns]
	if rolesAndBindings == nil {
		return nil
	}
	return rolesAndBindings.Roles
}

// ListServiceRoleBindings returns the ServiceRoleBindings in the given namespace.
func (policy *AuthorizationPolicies) ListServiceRoleBindings(ns string) map[string][]*rbacproto.ServiceRoleBinding {
	if policy == nil {
		return map[string][]*rbacproto.ServiceRoleBinding{}
	}

	rolesAndBindings := policy.NamespaceToV1alpha1Policies[ns]
	if rolesAndBindings == nil || rolesAndBindings.Bindings == nil {
		return map[string][]*rbacproto.ServiceRoleBinding{}
	}

	return rolesAndBindings.Bindings
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

func (policy *AuthorizationPolicies) addServiceRoles(roles []Config) {
	if policy == nil {
		return
	}
	for _, role := range roles {
		if policy.NamespaceToV1alpha1Policies[role.Namespace] == nil {
			policy.NamespaceToV1alpha1Policies[role.Namespace] = &RolesAndBindings{
				Bindings: map[string][]*rbacproto.ServiceRoleBinding{},
			}
		}
		rolesAndBindings := policy.NamespaceToV1alpha1Policies[role.Namespace]
		config := ServiceRoleConfig{
			Name:        role.Name,
			ServiceRole: role.Spec.(*rbacproto.ServiceRole),
		}
		rolesAndBindings.Roles = append(rolesAndBindings.Roles, config)
	}
}

func (policy *AuthorizationPolicies) addServiceRoleBindings(bindings []Config) {
	if policy == nil {
		return
	}

	for _, binding := range bindings {
		spec := binding.Spec.(*rbacproto.ServiceRoleBinding)
		name := spec.RoleRef.Name
		if name == "" {
			rbacLog.Errorf("ignored invalid binding %s in %s with empty RoleRef.Name",
				binding.Name, binding.Namespace)
			return
		}

		if policy.NamespaceToV1alpha1Policies[binding.Namespace] == nil {
			policy.NamespaceToV1alpha1Policies[binding.Namespace] = &RolesAndBindings{
				Bindings: map[string][]*rbacproto.ServiceRoleBinding{},
			}
		}
		rolesAndBindings := policy.NamespaceToV1alpha1Policies[binding.Namespace]
		rolesAndBindings.Bindings[name] = append(
			rolesAndBindings.Bindings[name], binding.Spec.(*rbacproto.ServiceRoleBinding))
	}
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

// isInRbacTargetList checks if a given service and namespace is included in the RbacConfig target.
func isInRbacTargetList(serviceHostname string, namespace string, target *rbacproto.RbacConfig_Target) bool {
	if target == nil {
		return false
	}
	for _, ns := range target.Namespaces {
		if namespace == ns {
			return true
		}
	}
	for _, service := range target.Services {
		if service == serviceHostname {
			return true
		}
	}
	return false
}
