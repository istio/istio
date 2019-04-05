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
	"fmt"

	rbacproto "istio.io/api/rbac/v1alpha1"
	istiolog "istio.io/istio/pkg/log"
)

const (
	// DefaultRbacConfigName is the name of the mesh global RbacConfig name. Only RbacConfig with this
	// name will be considered.
	DefaultRbacConfigName = "default"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

// RolesAndBindings stores the the ServiceRole and ServiceRoleBinding in the same namespace.
type RolesAndBindings struct {
	// ServiceRoles in the same namespace.
	Roles []Config

	// Maps from ServiceRole name to its associated ServiceRoleBindings.
	RoleNameToBindings map[string][]*rbacproto.ServiceRoleBinding
}

// AuthorizationPolicyConfig stores the AuthorizationPolicy and its name.
type AuthorizationPolicyConfig struct {
	Name   string
	Policy *rbacproto.AuthorizationPolicy
}

// AuthorizationConfigV2 stores a list of AuthorizationPolicyConfig and ServiceRole in a given namespace.
type AuthorizationConfigV2 struct {
	// A list of AuthorizationPolicyConfig.
	AuthzPolicies []*AuthorizationPolicyConfig

	// Maps from name to ServiceRole.
	NameToServiceRoles map[string]*rbacproto.ServiceRole
}

// AuthorizationPolicies stores all authorization policies (i.e. ServiceRole, ServiceRoleBinding and
// RbacConfig) according to its namespace.
type AuthorizationPolicies struct {
	// Maps from namespace to ServiceRole and ServiceRoleBindings.
	NamespaceToPolicies map[string]*RolesAndBindings

	// Maps from namespace to AuthorizationConfigV2.
	NamespaceToAuthorizationConfigV2 map[string]*AuthorizationConfigV2

	// The mesh global RbacConfig.
	RbacConfig *rbacproto.RbacConfig

	// True if using RBAC v2 (i.e. AuthorizationPolicy and no ServiceRoleBinding).
	IsRbacV2 bool
}

func (policy *AuthorizationPolicies) addServiceRole(role *Config) {
	if role == nil || role.Spec.(*rbacproto.ServiceRole) == nil {
		return
	}
	if policy.NamespaceToPolicies == nil {
		policy.NamespaceToPolicies = map[string]*RolesAndBindings{}
	}
	if policy.NamespaceToPolicies[role.Namespace] == nil {
		policy.NamespaceToPolicies[role.Namespace] = &RolesAndBindings{
			Roles:              []Config{},
			RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
		}
	}
	rolesAndBindings := policy.NamespaceToPolicies[role.Namespace]
	rolesAndBindings.Roles = append(rolesAndBindings.Roles, *role)

	// Initialize NameToServiceRoles for Authz v2.
	if policy.NamespaceToAuthorizationConfigV2 == nil {
		policy.NamespaceToAuthorizationConfigV2 = map[string]*AuthorizationConfigV2{}
	}
	if policy.NamespaceToAuthorizationConfigV2[role.Namespace] == nil {
		policy.NamespaceToAuthorizationConfigV2[role.Namespace] = &AuthorizationConfigV2{
			AuthzPolicies:      []*AuthorizationPolicyConfig{},
			NameToServiceRoles: map[string]*rbacproto.ServiceRole{},
		}
	}
	authzV2 := policy.NamespaceToAuthorizationConfigV2[role.Namespace]
	authzV2.NameToServiceRoles[role.Name] = role.Spec.(*rbacproto.ServiceRole)
}

func (policy *AuthorizationPolicies) addServiceRoleBinding(binding *Config) {
	if binding == nil || binding.Spec.(*rbacproto.ServiceRoleBinding) == nil {
		return
	}
	name := binding.Spec.(*rbacproto.ServiceRoleBinding).RoleRef.Name
	if name == "" {
		rbacLog.Errorf("ignored invalid binding %s in %s with empty RoleRef.Name",
			binding.Name, binding.Namespace)
		return
	}
	if policy.NamespaceToPolicies == nil {
		policy.NamespaceToPolicies = map[string]*RolesAndBindings{}
	}
	if policy.NamespaceToPolicies[binding.Namespace] == nil {
		policy.NamespaceToPolicies[binding.Namespace] = &RolesAndBindings{
			Roles:              []Config{},
			RoleNameToBindings: map[string][]*rbacproto.ServiceRoleBinding{},
		}
	}
	rolesAndBindings := policy.NamespaceToPolicies[binding.Namespace]
	if rolesAndBindings.RoleNameToBindings[name] == nil {
		rolesAndBindings.RoleNameToBindings[name] = []*rbacproto.ServiceRoleBinding{}
	}
	rolesAndBindings.RoleNameToBindings[name] = append(
		rolesAndBindings.RoleNameToBindings[name], binding.Spec.(*rbacproto.ServiceRoleBinding))
}

func (policy *AuthorizationPolicies) addAuthorizationPolicy(authzPolicy *Config) {
	if authzPolicy == nil || authzPolicy.Spec.(*rbacproto.AuthorizationPolicy) == nil {
		return
	}

	// Initialize AuthzPolicies for authz v2.
	if policy.NamespaceToAuthorizationConfigV2 == nil {
		policy.NamespaceToAuthorizationConfigV2 = map[string]*AuthorizationConfigV2{}
	}
	if policy.NamespaceToAuthorizationConfigV2[authzPolicy.Namespace] == nil {
		policy.NamespaceToAuthorizationConfigV2[authzPolicy.Namespace] = &AuthorizationConfigV2{
			AuthzPolicies:      []*AuthorizationPolicyConfig{},
			NameToServiceRoles: map[string]*rbacproto.ServiceRole{},
		}
	}
	authzV2 := policy.NamespaceToAuthorizationConfigV2[authzPolicy.Namespace]
	authzV2.AuthzPolicies = append(authzV2.AuthzPolicies, &AuthorizationPolicyConfig{
		Name:   authzPolicy.Name,
		Policy: authzPolicy.Spec.(*rbacproto.AuthorizationPolicy),
	})
}

// AddConfig adds a config of type ServiceRole, ServiceRoleBinding or AuthorizationPolicy to
// AuthorizationPolicies.
func (policy *AuthorizationPolicies) AddConfig(cfgs ...*Config) {
	for _, cfg := range cfgs {
		if cfg == nil {
			continue
		}
		switch cfg.Spec.(type) {
		case *rbacproto.ServiceRole:
			policy.addServiceRole(cfg)
		case *rbacproto.ServiceRoleBinding:
			policy.addServiceRoleBinding(cfg)
		case *rbacproto.AuthorizationPolicy:
			policy.addAuthorizationPolicy(cfg)
		}
	}
}

// RolesForNamespace returns the ServiceRole configs in the given namespace. This function always
// return a non nil slice.
func (policy *AuthorizationPolicies) RolesForNamespace(ns string) []Config {
	if policy == nil || policy.NamespaceToPolicies == nil {
		return []Config{}
	}

	rolesAndBindings := policy.NamespaceToPolicies[ns]
	if rolesAndBindings == nil || rolesAndBindings.Roles == nil {
		return []Config{}
	}
	return rolesAndBindings.Roles
}

// RoleToBindingsForNamespace returns the mapping from ServiceRole name to its associated ServiceRoleBindings.
// This function always return a non nil map.
func (policy *AuthorizationPolicies) RoleToBindingsForNamespace(ns string) map[string][]*rbacproto.ServiceRoleBinding {
	if policy == nil || policy.NamespaceToPolicies == nil {
		return map[string][]*rbacproto.ServiceRoleBinding{}
	}

	rolesAndBindings := policy.NamespaceToPolicies[ns]
	if rolesAndBindings == nil || rolesAndBindings.RoleNameToBindings == nil {
		return map[string][]*rbacproto.ServiceRoleBinding{}
	}
	return rolesAndBindings.RoleNameToBindings
}

// RoleForNameAndNamespace returns a ServiceRole from this namespace, given the ServiceRole name and namespace.
// This function always return a non nil struct instance.
func (policy *AuthorizationPolicies) RoleForNameAndNamespace(roleName, ns string) *rbacproto.ServiceRole {
	if policy == nil || policy.NamespaceToAuthorizationConfigV2 == nil {
		return &rbacproto.ServiceRole{}
	}
	nsToAuthzConfigV2 := policy.NamespaceToAuthorizationConfigV2[ns]
	if nsToAuthzConfigV2 == nil {
		return &rbacproto.ServiceRole{}
	}
	serviceRole, exist := nsToAuthzConfigV2.NameToServiceRoles[roleName]
	if !exist {
		return &rbacproto.ServiceRole{}
	}
	return serviceRole
}

// NewAuthzPolicies returns the AuthorizationPolicies constructed from raw authorization policies by
// storing policies into different namespaces.
func NewAuthzPolicies(env *Environment) (*AuthorizationPolicies, error) {
	// Get the ClusterRbacConfig first, if not found then fallback to get the RbacConfig.
	rbacConfig := env.IstioConfigStore.ClusterRbacConfig()
	if rbacConfig == nil {
		rbacConfig = env.IstioConfigStore.RbacConfig()
		if rbacConfig == nil {
			return nil, nil
		}
	}
	policy := &AuthorizationPolicies{
		NamespaceToPolicies:              map[string]*RolesAndBindings{},
		NamespaceToAuthorizationConfigV2: map[string]*AuthorizationConfigV2{},
		RbacConfig:                       rbacConfig.Spec.(*rbacproto.RbacConfig),
		IsRbacV2:                         false,
	}

	roles, err := env.List(ServiceRole.Type, NamespaceAll)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		policy.AddConfig(&role)
	}

	bindings, err := env.List(ServiceRoleBinding.Type, NamespaceAll)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		policy.AddConfig(&binding)
	}

	v2Policies, err := env.List(AuthorizationPolicy.Type, NamespaceAll)
	if err != nil {
		return nil, err
	}
	if len(v2Policies) > 0 {
		if len(bindings) > 0 {
			return nil, fmt.Errorf("had both AuthorizationPolicy and ServiceRoleBinding")
		}
		policy.IsRbacV2 = true
	}
	for _, v2Policy := range v2Policies {
		policy.AddConfig(&v2Policy)
	}

	return policy, nil
}
