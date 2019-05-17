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

package auth

import (
	"fmt"
	"strings"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

type Validator struct {
	PolicyFiles          []string
	RoleKeyToServiceRole map[string]model.Config
	serviceRoleBindings  []model.Config
	Report               strings.Builder
}

const (
	RoleNotFound        = "serviceRoleNotFound: %q used by ServiceRoleBinding %q at namespace %q.\n"
	RoleNotUsed         = "serviceRoleNotUsed: ServiceRole %q at namespace %q.\n"
	PolicyValid         = "Authorization policy is valid.\n"
	RoleMissing         = "no ServiceRole found for validation"
	BindingMissing      = "no ServiceRoleBinding found for validation"
	ValidButNoRBACFound = "Valid (no Authorization policy found).\n"
)

// CheckAndReport checks for Istio authentication and authorization mis-usage.
func (v *Validator) CheckAndReport() error {
	err := v.getRoleAndBindingLists()
	if err != nil {
		return err
	}
	if v.Report.String() == ValidButNoRBACFound {
		return nil
	}
	v.CheckAndReportRBAC()
	if v.Report.String() == "" {
		v.Report.WriteString(GetPolicyValidReport())
	}
	return nil
}

func (v *Validator) CheckAndReportRBAC() {
	usedRoleNames := map[string]bool{}
	// Check if ServiceRoleBinding is using an non existent ServiceRole.
	for _, binding := range v.serviceRoleBindings {
		bindingSpec := binding.Spec.(*rbacproto.ServiceRoleBinding)
		namespace := binding.Namespace
		roleName := bindingSpec.RoleRef.Name
		roleKey := getRoleKey(namespace, roleName)
		if v.doesRoleExist(roleKey) {
			usedRoleNames[roleKey] = true
		} else {
			v.Report.WriteString(GetRoleNotFoundReport(roleName, binding.Name, namespace))
		}
	}
	// Check if ServiceRole is unused.
	for roleKey := range v.RoleKeyToServiceRole {
		if _, found := usedRoleNames[roleKey]; !found {
			namespace, roleName := getNamespaceAndRoleNameFromRoleKey(roleKey)
			v.Report.WriteString(GetRoleNotUsedReport(roleName, namespace))
		}
	}
}

// doesRoleExist check if a role exist in the given namespace in the provided policy file.
func (v *Validator) doesRoleExist(roleKey string) bool {
	if _, found := v.RoleKeyToServiceRole[roleKey]; found {
		return true
	}
	return false
}

// getRoleKey joins namespace and role name with a forward slash and returns the result.
func getRoleKey(namespace, roleName string) string {
	return fmt.Sprintf("%s/%s", namespace, roleName)
}

// getNamespaceAndRoleNameFromRoleKey returns namespace and role name from the given role key.
func getNamespaceAndRoleNameFromRoleKey(roleKey string) (string, string) {
	foo := strings.Split(roleKey, "/")
	return foo[0], foo[1]
}

// getRoleAndBindingLists get roles and bindings from the provided files to the appropriate data structures for the validator.
func (v *Validator) getRoleAndBindingLists() error {
	configsFromFiles, err := getConfigsFromFiles(v.PolicyFiles)
	if err != nil {
		return err
	}
	for _, role := range configsFromFiles[model.ServiceRole.Type] {
		roleKey := getRoleKey(role.Namespace, role.Name)
		v.RoleKeyToServiceRole[roleKey] = role
	}
	v.serviceRoleBindings = configsFromFiles[model.ServiceRoleBinding.Type]
	if len(v.serviceRoleBindings) == 0 && len(v.RoleKeyToServiceRole) == 0 {
		v.Report.WriteString(ValidButNoRBACFound)
		return nil
	}
	if len(v.serviceRoleBindings) == 0 {
		return fmt.Errorf(BindingMissing)
	}
	if len(v.RoleKeyToServiceRole) == 0 {
		return fmt.Errorf(RoleMissing)
	}
	return nil
}

func GetRoleNotFoundReport(roleName, bindingName, namespace string) string {
	return fmt.Sprintf(RoleNotFound, roleName, bindingName, namespace)
}

func GetRoleNotUsedReport(roleName, namespace string) string {
	return fmt.Sprintf(RoleNotUsed, roleName, namespace)
}

func GetPolicyValidReport() string {
	return PolicyValid
}
