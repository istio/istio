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
	"fmt"
	"strings"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/authorization"
)

// authorizer interface
type authorizer interface {
	CheckPermission(inst *authorization.Instance, logger adapter.Logger) (bool, error)
}

// RoleInfo contains information about a ServiceRole and associated ServiceRoleBindings.
type RoleInfo struct {
	// ServiceRole proto definition
	Info *rbacproto.ServiceRole

	// A set of ServiceRoleBindings that refer to this role.
	Bindings map[string]*rbacproto.ServiceRoleBinding
}

// RolesByName maps role name to role info
type RolesByName map[string]*RoleInfo

// RolesMapByNamespace maps namespace to a set of Roles in the namespace
type RolesMapByNamespace map[string]RolesByName

// ConfigStore contains all ServiceRole and ServiceRoleBinding information.
// ConfigStore implements authorizer interface.
type ConfigStore struct {
	// All the Roles organized per namespace.
	Roles RolesMapByNamespace
}

// Create a RoleInfo object.
func newRoleInfo(spec *rbacproto.ServiceRole) *RoleInfo {
	return &RoleInfo{
		Info: spec,
	}
}

// Set a binding for a given Service role.
func (ri *RoleInfo) setBinding(name string, spec *rbacproto.ServiceRoleBinding) {
	if ri == nil {
		return
	}
	if ri.Bindings == nil {
		ri.Bindings = make(map[string]*rbacproto.ServiceRoleBinding)
	}
	ri.Bindings[name] = spec
}

// Update roles in the RBAC store.
func (rs *ConfigStore) changeRoles(roles RolesMapByNamespace) {
	rs.Roles = roles
}

// AddServiceRole adds a new ServiceRole to RolesMapByNamespace with the specified name and namespace.
// Return nil if added successfully, otherwise return an error.
func (rs *RolesMapByNamespace) AddServiceRole(name, namespace string, proto *rbacproto.ServiceRole) error {
	if rs == nil {
		return nil
	}

	rolesByName := (*rs)[namespace]
	if rolesByName == nil {
		rolesByName = make(RolesByName)
		(*rs)[namespace] = rolesByName
	}

	if _, present := rolesByName[name]; present {
		return fmt.Errorf("duplicate ServiceRole: %v", name)
	}
	rolesByName[name] = newRoleInfo(proto)
	return nil
}

// AddServiceRoleBinding adds a new ServiceRoleBinding to RolesMapByNamespace with the specified
// name and namespace. Return nil if added successfully, otherwise return an error.
func (rs *RolesMapByNamespace) AddServiceRoleBinding(name, namespace string, proto *rbacproto.ServiceRoleBinding) error {
	if rs == nil {
		return nil
	}

	if proto.RoleRef.Kind != serviceRoleKind {
		return fmt.Errorf("roleBinding %s has role kind %s, expected %s",
			name, proto.RoleRef.Kind, serviceRoleKind)
	}

	rolesByName := (*rs)[namespace]
	if rolesByName == nil {
		return fmt.Errorf("roleBinding %s is in a namespace (%s) that no valid role is defined",
			name, namespace)
	}

	refName := proto.RoleRef.Name
	roleInfo := rolesByName[refName]
	if refName == "" {
		return fmt.Errorf("roleBinding %s does not refer to a valid role name", refName)
	}
	if roleInfo == nil {
		return fmt.Errorf("roleBinding %s is bound to a role that does not exist %s", name, refName)
	}

	if _, present := roleInfo.Bindings[name]; present {
		return fmt.Errorf("duplicate RoleBinding: %v", name)
	}
	roleInfo.setBinding(name, proto)
	return nil
}

// ListPermissions returns list of AccessRule(permission) that matches subjects.
func (rs *ConfigStore) ListPermissions(subject SubjectArgs, action ActionArgs) (*map[string]*rbacproto.ServiceRole, error) {
	instance, err := createInstance(subject, action)
	if err != nil {
		return nil, err
	}

	logger := istioctlLogger{}

	namespace := instance.Action.Namespace
	if namespace == "" {
		return nil, logger.Errorf("Missing namespace")
	}

	accessRules := make(map[string]*rbacproto.ServiceRole)

	rn := rs.Roles[namespace]
	if rn == nil {
		return &accessRules, nil
	}

	for roleName, roleInfo := range rn {
		logger.Infof("Checking role: %s", roleName)

		for _, serviceRoleBinding := range roleInfo.Bindings {
			for _, subject := range serviceRoleBinding.Subjects {
				if checkSubjectsMatch(subject, instance) {
					accessRules[roleName] = rn[roleName].Info
					break
				}
			}

			if _, ok := accessRules[roleName]; ok {
				break
			}
		}
	}

	return &accessRules, nil
}

// ListSubjects searches subjects that allowed to use the given service role
func (rs *ConfigStore) ListSubjects(subject SubjectArgs, action ActionArgs) (*[]*rbacproto.Subject, error) {
	instance, err := createInstance(subject, action)
	if err != nil {
		return nil, err
	}

	logger := istioctlLogger{}

	namespace := instance.Action.Namespace
	if namespace == "" {
		return nil, logger.Errorf("Missing namespace")
	}

	serviceName := instance.Action.Service
	if serviceName == "" {
		return nil, logger.Errorf("Missing service")
	}

	path := instance.Action.Path
	if path == "" {
		return nil, logger.Errorf("Missing path")
	}

	method := instance.Action.Method
	if method == "" {
		return nil, logger.Errorf("Missing method")
	}

	subjects := make([]*rbacproto.Subject, 0)

	rn := rs.Roles[namespace]
	if rn == nil {
		return &subjects, nil
	}

	for roleName, roleInfo := range rn {
		eligibleRole := false

		logger.Infof("Checking role: %s", roleName)
		rules := roleInfo.Info.GetRules()
		for _, rule := range rules {
			if matchRule(serviceName, path, method, instance.Action.Properties, rule, logger) {
				eligibleRole = true
				logger.Debugf("rule matched")
				break
			}
		}

		if !eligibleRole {
			logger.Infof("role %s is not eligible", roleName)
			continue
		}

		for _, serviceRoleBinding := range roleInfo.Bindings {
			for _, subject := range serviceRoleBinding.Subjects {
				subjects = append(subjects, subject)
			}
		}
	}

	return &subjects, nil
}

// CheckPermission checks permission for a given request. This is the main API called
// by RBAC adapter at runtime to authorize requests.
func (rs *ConfigStore) CheckPermission(inst *authorization.Instance, logger adapter.Logger) (bool, error) {
	namespace := inst.Action.Namespace
	if namespace == "" {
		return false, logger.Errorf("Missing namespace")
	}

	serviceName := inst.Action.Service
	if serviceName == "" {
		return false, logger.Errorf("Missing service")
	}

	path := inst.Action.Path
	if path == "" {
		return false, logger.Errorf("Missing path")
	}

	method := inst.Action.Method
	if method == "" {
		return false, logger.Errorf("Missing method")
	}

	extraAct := inst.Action.Properties

	user := inst.Subject.User
	groups := inst.Subject.Groups

	instSub := inst.Subject.Properties

	rn := rs.Roles[namespace]
	if rn == nil {
		return false, nil
	}

	for rolename, roleInfo := range rn {
		eligibleRole := false
		logger.Infof("Checking role: %s", rolename)
		rules := roleInfo.Info.GetRules()
		for _, rule := range rules {
			if matchRule(serviceName, path, method, extraAct, rule, logger) {
				eligibleRole = true
				logger.Debugf("rule matched")
				break
			}
		}
		if !eligibleRole {
			logger.Infof("role %s is not eligible", rolename)
			continue
		}
		logger.Infof("role %s is eligible", rolename)
		bindings := roleInfo.Bindings
		for _, binding := range bindings {
			logger.Debugf("Checking binding %v", binding)
			subjects := binding.GetSubjects()
			for _, subject := range subjects {
				foundMatch := false
				if subject.GetUser() != "" {
					if subject.GetUser() == "*" || subject.GetUser() == user {
						foundMatch = true
					} else {
						// Found a mismatch, try next subject.
						continue
					}
				}
				if subject.GetGroup() != "" {
					if subject.GetGroup() == "*" || subject.GetGroup() == groups {
						foundMatch = true
					} else {
						// Found a mismatch, try next subject.
						continue
					}
				}
				subProp := subject.GetProperties()
				if len(subProp) != 0 {
					if checkSubjectProperties(instSub, subProp) {
						foundMatch = true
					} else {
						// Found a mismatch, try next subject.
						continue
					}
				}
				if foundMatch {
					logger.Debugf("binding matched")
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Helper function to check whether or not a request matches a rule in a ServiceRole specification.
func matchRule(serviceName string, path string, method string, extraAct map[string]interface{}, rule *rbacproto.AccessRule, logger adapter.Logger) bool {
	services := rule.GetServices()
	paths := rule.GetPaths()
	methods := rule.GetMethods()
	constraints := rule.GetConstraints()
	logger.Infof("Checking rule: services %v, path %v, method %v, constraints %v", services, paths, methods, constraints)

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
	if !strings.HasSuffix(pattern, "*") {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(a, pattern)
}

// Helper function to check if string "a" prefix matches "pattern".
func suffixMatch(a string, pattern string) bool {
	if !strings.HasPrefix(pattern, "*") {
		return false
	}
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

// checkSubjectsMatch returns true if rule subjects and instance subjects are matched. Otherwise
// returns false.
func checkSubjectsMatch(subject *rbacproto.Subject, instance *authorization.Instance) bool {
	if instance.Subject.User == "" && instance.Subject.Groups == "" && len(instance.Subject.Properties) == 0 {
		return false
	}

	if subject.GetUser() != "" && !stringMatch(instance.Subject.User, []string{subject.GetUser()}) {
		return false
	}

	if subject.GetGroup() != "" && !stringMatch(instance.Subject.Groups, []string{subject.GetGroup()}) {
		return false
	}

	if len(subject.GetProperties()) > 0 && !checkSubjectProperties(instance.Subject.Properties, subject.GetProperties()) {
		return false
	}

	return true
}

// checkSubjectProperties checks if all properties defined for the subject are satisfied
// by the properties from the request.
func checkSubjectProperties(properties map[string]interface{}, subject map[string]string) bool {
	for sn, sv := range subject {
		if properties[sn] == nil || !valueMatch(properties[sn], sv) {
			return false
		}
	}
	return true
}
