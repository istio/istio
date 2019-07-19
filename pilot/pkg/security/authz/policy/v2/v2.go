// Copyright 2019 Istio Authors
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

package v2

import (
	"fmt"
	"strings"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	istiolog "istio.io/pkg/log"
)

const (
	// rootNamespacePrefix is the prefix of the root namespace. This is used to refer to ServiceRoles
	// that are defined in the root namespace.
	// For more details, check out root_namespace field at https://github.com/istio/api/blob/master/mesh/v1alpha1/config.proto
	rootNamespacePrefix = "/"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type v2Generator struct {
	serviceMetadata           *authz_model.ServiceMetadata
	authzPolicies             *model.AuthorizationPolicies
	isGlobalPermissiveEnabled bool
}

func NewGenerator(
	serviceMetadata *authz_model.ServiceMetadata,
	authzPolicies *model.AuthorizationPolicies,
	isGlobalPermissiveEnabled bool) policy.Generator {
	return &v2Generator{
		serviceMetadata:           serviceMetadata,
		authzPolicies:             authzPolicies,
		isGlobalPermissiveEnabled: isGlobalPermissiveEnabled,
	}
}

func (b *v2Generator) Generate(forTCPFilter bool) *http_config.RBAC {
	rbacLog.Debugf("building v2 policy")

	rbac := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	if b.isGlobalPermissiveEnabled {
		// TODO(pitlv2109): Handle permissive mode in the future.
		rbacLog.Errorf("ignored global permissive mode: not implemented for v2 policy.")
	}

	serviceMetadata := b.serviceMetadata
	authzPolicies := b.authzPolicies

	namespace := serviceMetadata.GetNamespace()
	if authzPolicies.NamespaceToAuthorizationConfigV2 == nil {
		return &http_config.RBAC{Rules: rbac}
	}
	var authzConfigV2, present = authzPolicies.NamespaceToAuthorizationConfigV2[namespace]
	if !present {
		return &http_config.RBAC{Rules: rbac}
	}
	for _, authzPolicy := range authzConfigV2.AuthzPolicies {
		workloadLabels := model.LabelsCollection{serviceMetadata.Labels}
		policySelector := authzPolicy.Policy.WorkloadSelector.GetLabels()
		if !(workloadLabels.IsSupersetOf(policySelector)) {
			// Skip if the workload labels is not a superset of the policy selector (i.e. the workload
			// is not selected by the policy).
			continue
		}

		for i, binding := range authzPolicy.Policy.Allow {
			// TODO: optimize for multiple bindings referring to the same role.
			roleName, role := roleForBinding(binding, namespace, authzPolicies)
			bindings := []*istio_rbac.ServiceRoleBinding{binding}
			if p := b.generatePolicy(role, bindings, forTCPFilter); p != nil {
				rbacLog.Debugf("generated policy for role: %s", roleName)
				policyName := fmt.Sprintf("authz-[%s]-allow[%d]", authzPolicy.Name, i)
				rbac.Policies[policyName] = p
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}

// TODO: refactor this into model.AuthorizationPolicies.
func roleForBinding(binding *istio_rbac.ServiceRoleBinding, namespace string, ap *model.AuthorizationPolicies) (string, *istio_rbac.ServiceRole) {
	var role *istio_rbac.ServiceRole
	var name string
	if binding.RoleRef != nil {
		role = ap.RoleForNameAndNamespace(binding.RoleRef.Name, namespace)
		name = binding.RoleRef.Name
	} else if binding.Role != "" {
		if strings.HasPrefix(binding.Role, rootNamespacePrefix) {
			globalRoleName := strings.TrimPrefix(binding.Role, rootNamespacePrefix)
			role = ap.RoleForNameAndNamespace(globalRoleName, model.DefaultMeshConfig().RootNamespace)
		} else {
			role = ap.RoleForNameAndNamespace(binding.Role, namespace)
		}
		name = binding.Role
	} else if len(binding.Actions) > 0 {
		role = &istio_rbac.ServiceRole{Rules: binding.Actions}
		name = "%s-inline-role"
	}
	return name, role
}

func (b *v2Generator) generatePolicy(role *istio_rbac.ServiceRole, bindings []*istio_rbac.ServiceRoleBinding, forTCPFilter bool) *envoy_rbac.Policy {
	if role == nil || len(bindings) == 0 {
		return nil
	}

	m := authz_model.NewModel(role, bindings)
	return m.Generate(b.serviceMetadata, forTCPFilter)
}
