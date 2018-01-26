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

package rbac

import (
	"strings"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/authorization"
)

// authorizer interface
type authorizer interface {
	CheckPermission(inst *authorization.Instance, env adapter.Env) (bool, error)
}

// Information about a ServiceRole and associted ServiceRoleBindings
type roleInfo struct {
	// ServiceRole proto definition
	info *rbacproto.ServiceRole

	// A set of ServiceRoleBindings that refer to this role.
	bindings map[string]*rbacproto.ServiceRoleBinding
}

// maps role name to role info
type rolesByName map[string]*roleInfo

// maps namespace to a set of roles in the namespace
type rolesMapByNamespace map[string]rolesByName

// configStore contains all ServiceRole and ServiceRoleBinding information.
// configStore implements authorizer interface.
type configStore struct {
	// All the roles organized per namespace.
	roles rolesMapByNamespace
}

// Create a RoleInfo object.
func newRoleInfo(spec *rbacproto.ServiceRole) *roleInfo {
	return &roleInfo{
		info: spec,
	}
}

// Set a binding for a given Service role.
func (ri *roleInfo) setBinding(name string, spec *rbacproto.ServiceRoleBinding) {
	if ri == nil {
		return
	}
	if ri.bindings == nil {
		ri.bindings = make(map[string]*rbacproto.ServiceRoleBinding)
	}
	ri.bindings[name] = spec
}

// Update roles in the RBAC store.
func (rs *configStore) changeRoles(roles rolesMapByNamespace) {
	rs.roles = roles
}

// CheckPermission checks permission for a given request. This is the main API called
// by RBAC adapter at runtime to authorize requests.
func (rs *configStore) CheckPermission(inst *authorization.Instance, env adapter.Env) (bool, error) {
	namespace := inst.Action.Namespace
	if namespace == "" {
		return false, env.Logger().Errorf("Missing namespace")
	}

	serviceName := inst.Action.Service
	if serviceName == "" {
		return false, env.Logger().Errorf("Missing service")
	}

	path := inst.Action.Path
	if path == "" {
		return false, env.Logger().Errorf("Missing path")
	}

	method := inst.Action.Method
	if method == "" {
		return false, env.Logger().Errorf("Missing method")
	}

	extraAct := inst.Action.Properties

	user := inst.Subject.User
	groups := inst.Subject.Groups

	instSub := inst.Subject.Properties

	rn := rs.roles[namespace]
	if rn == nil {
		return false, nil
	}

	for rolename, roleInfo := range rn {
		eligibleRole := false
		env.Logger().Infof("Checking role: %s", rolename)
		rules := roleInfo.info.GetRules()
		for _, rule := range rules {
			if matchRule(serviceName, path, method, extraAct, rule, env) {
				eligibleRole = true
				break
			}
		}
		if !eligibleRole {
			env.Logger().Infof("role %s is not eligible", rolename)
			continue
		}
		env.Logger().Infof("role %s is eligible", rolename)
		bindings := roleInfo.bindings
		for _, binding := range bindings {
			subjects := binding.GetSubjects()
			for _, subject := range subjects {
				if subject.GetUser() != "" && subject.GetUser() != user {
					continue
				}
				if subject.GetGroup() != "" && subject.GetGroup() != groups {
					continue
				}
				if checkSubject(instSub, subject.GetProperties()) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Helper function to check whether or not a request matches a rule in a ServiceRole specification.
func matchRule(serviceName string, path string, method string, extraAct map[string]interface{}, rule *rbacproto.AccessRule, env adapter.Env) bool {
	services := rule.GetServices()
	paths := rule.GetPaths()
	methods := rule.GetMethods()
	constraints := rule.GetConstraints()
	env.Logger().Infof("Checking rule: services %v, path %v, method %v, constraints %v", services, paths, methods, constraints)

	if stringMatch(serviceName, services) &&
		(paths == nil || stringMatch(path, paths)) &&
		stringMatch(method, methods) &&
		checkConstraints(extraAct, constraints) {
		return true
	}
	return false
}

// Helper function to check if a string is in a list.
// We support four types of string matches:
// 1. Exact match.
// 2. Wild character match. "*" matches any string.
// 3. Prefix match. For example, "book*" matches "bookstore", "bookshop", etc.
// 4. Suffix match. For example, "*/review" matches "/bookstore/review", "/products/review", etc.
func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" || prefixMatch(a, s) || suffixMatch(a, s) {
			return true
		}
	}
	return false
}

// Helper function to check if string "a" prefix matches "pattern".
func prefixMatch(a string, pattern string) bool {
	pattern = strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(a, pattern)
}

// Helper function to check if string "a" prefix matches "pattern".
func suffixMatch(a string, pattern string) bool {
	pattern = strings.TrimPrefix(pattern, "*")
	return strings.HasSuffix(a, pattern)
}

// Helper function to check if a given string value
// is in a list of strings.
func valueInList(value interface{}, list []string) bool {
	if str, ok := value.(string); ok {
		return stringMatch(str, list)
	}
	return false
}

// Check if all constraints in a rule can be satisfied by the properties from the request.
func checkConstraints(properties map[string]interface{}, constraints []*rbacproto.AccessRule_Constraint) bool {
	for _, constraint := range constraints {
		foundMatch := false
		for pn, pv := range properties {
			if pn == constraint.GetKey() && valueInList(pv, constraint.GetValues()) {
				foundMatch = true
				break
			}
		}
		if !foundMatch {
			// constraints of the rule is not satisfied, skip the rule
			return false
		}
	}
	return true
}

// Check if a given value is equal to a string.
func valueMatch(a interface{}, b string) bool {
	if str, ok := a.(string); ok {
		return str == b
	}
	return false
}

// Check if all properties defined for the subject are satisfied by the properties from the request.
func checkSubject(properties map[string]interface{}, subject map[string]string) bool {
	for sn, sv := range subject {
		if properties[sn] == nil || !valueMatch(properties[sn], sv) {
			return false
		}
	}
	return true
}
