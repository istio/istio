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
	"istio.io/istio/pkg/log"
)

const (
	// ServiceRoleKind defines the config kind name of ServiceRole.
	serviceRoleKind = "ServiceRole"
)

// SubjectArgs contains information about the subject of a request.
type SubjectArgs struct {
	User       string
	Groups     string
	Properties []string
}

// ActionArgs contains information about the detail of a request.
type ActionArgs struct {
	Namespace  string
	Service    string
	Method     string
	Path       string
	Properties []string
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

	if roleInfo.Bindings == nil {
		roleInfo.Bindings = make(map[string]*rbacproto.ServiceRoleBinding)
	}
	roleInfo.Bindings[name] = proto

	return nil
}

func convertProperties(arguments []string) map[string]string {
	properties := map[string]string{}
	for _, arg := range arguments {
		// Use the part before the first = as key and the remaining part as a string value, this is
		// the only supported format for now.
		split := strings.SplitN(arg, "=", 2)
		if len(split) != 2 {
			log.Debugf("invalid property %v, the format should be: key=value", arg)
			return nil
		}
		value, present := properties[split[0]]
		if present {
			log.Debugf("duplicate property %v, previous value %v", arg, value)
			return nil
		}
		properties[split[0]] = split[1]
	}
	return properties
}

// CheckPermission checks permission for a given subject and action.
// TODO(yangminzhu): Refactor and support checking RbacConfig.
func (rs *ConfigStore) CheckPermission(subject SubjectArgs, action ActionArgs) (bool, error) {
	if action.Namespace == "" {
		return false, fmt.Errorf("missing namespace")
	}
	if action.Service == "" {
		return false, fmt.Errorf("missing service")
	}
	if action.Path == "" {
		return false, fmt.Errorf("missing path")
	}
	if action.Method == "" {
		return false, fmt.Errorf("missing method")
	}

	rn := rs.Roles[action.Namespace]
	if rn == nil {
		return false, nil
	}

	for rolename, roleInfo := range rn {
		eligibleRole := false
		log.Debugf("Checking role: %s", rolename)
		rules := roleInfo.Info.GetRules()
		for _, rule := range rules {
			if matchRule(action.Service, action.Path, action.Method, convertProperties(action.Properties), rule) {
				eligibleRole = true
				log.Debugf("rule matched")
				break
			}
		}
		if !eligibleRole {
			log.Debugf("role %s is not eligible", rolename)
			continue
		}
		log.Debugf("role %s is eligible", rolename)
		bindings := roleInfo.Bindings
		for _, binding := range bindings {
			log.Debugf("Checking binding %v", binding)
			for _, sub := range binding.GetSubjects() {
				foundMatch := false
				if sub.GetUser() != "" {
					if sub.GetUser() == "*" || sub.GetUser() == subject.User {
						foundMatch = true
					} else {
						// Found a mismatch, try next sub.
						continue
					}
				}
				subProp := sub.GetProperties()
				if len(subProp) != 0 {
					if checkSubject(convertProperties(subject.Properties), subProp) {
						foundMatch = true
					} else {
						// Found a mismatch, try next sub.
						continue
					}
				}
				if foundMatch {
					log.Debugf("binding matched")
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Helper function to check whether or not a request matches a rule in a ServiceRole specification.
func matchRule(serviceName string, path string, method string, extraAct map[string]string, rule *rbacproto.AccessRule) bool {
	services := rule.GetServices()
	paths := rule.GetPaths()
	methods := rule.GetMethods()
	constraints := rule.GetConstraints()
	log.Debugf("Checking rule: services %v, path %v, method %v, constraints %v", services, paths, methods, constraints)

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

// Helper function to check if a given string value is in a list of strings.
func valueInList(value interface{}, list []string) bool {
	if str, ok := value.(string); ok {
		return stringMatch(str, list)
	}
	return false
}

// Check if all constraints in a rule can be satisfied by the properties from the request.
func checkConstraints(properties map[string]string, constraints []*rbacproto.AccessRule_Constraint) bool {
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

// Check if all properties defined for the subject are satisfied by the properties from the request.
func checkSubject(properties map[string]string, subject map[string]string) bool {
	for sn, sv := range subject {
		if properties[sn] != sv {
			return false
		}
	}
	return true
}
