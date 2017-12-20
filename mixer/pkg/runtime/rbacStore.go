// Copyright 2017 Istio Authors
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

package runtime

import (
	"github.com/golang/glog"

	cpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/template/authorization"
)

// RBAC interface
type Rbac interface {
	CheckPermission(inst *authorization.Instance) (bool, error)
}

// Information about a ServiceRole and associted ServiceRoleBindings
type roleInfo struct {
	// ServiceRole proto definition
	info *cpb.ServiceRole

	// A set of ServiceRoleBindings that refer to this role.
	bindings map[string]*cpb.ServiceRoleBinding
}

// maps role name to role info
type rolesByName map[string]*roleInfo

// maps namespace to a set of roles in the namespace
type rolesMapByNamespace map[string]rolesByName

// ServiceRoleKind defines the config kind name of ServiceRole.
const ServiceRoleKind = "servicerole"

// ServiceRoleBindingKind defines the config kind name of ServiceRoleBinding.
const ServiceRoleBindingKind = "servicerolebinding"

// RbacStore contains all ServiceRole and ServiceRoleBinding information.
// RbacStore implements Rbac interface.
type RbacStore struct {
	// All the roles organized per namespace.
	roles rolesMapByNamespace
}

// The single instance of RBAC store. It is initialized when runtime is initiated.
var RbacInstance RbacStore

// Create a RoleInfo object.
func newRoleInfo(spec *cpb.ServiceRole) *roleInfo {
	return &roleInfo{
		info: spec,
	}
}

// Set a binding for a given Service role.
func (ri *roleInfo) setBinding(name string, spec *cpb.ServiceRoleBinding) {
	if ri == nil {
		return
	}
	if ri.bindings == nil {
		ri.bindings = make(map[string]*cpb.ServiceRoleBinding)
	}
	ri.bindings[name] = spec
}

// Update roles in the RBAC store.
func (rs *RbacStore) changeRoles(roles rolesMapByNamespace) {
	rs.roles = roles
}

// Check permission for a given request. This is the main API called
// by RBAC adapter at runtime to authorize requests.
func (rs *RbacStore) CheckPermission(inst *authorization.Instance) (bool, error) {
	namespace := inst.Action.Namespace
	if namespace == "" {
		glog.Warningf("Missing namespace")
		return false, nil
	}

	serviceName := inst.Action.Service
	if serviceName == "" {
		glog.Warningf("Missing service")
		return false, nil
	}

	path := inst.Action.Path
	if path == "" {
		glog.Warningf("Missing path")
		return false, nil
	}

	method := inst.Action.Method
	if method == "" {
		glog.Warningf("Missing method")
		return false, nil
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
		glog.Infof("Checking role: %s", rolename)
		rules := roleInfo.info.GetRules()
		for _, rule := range rules {
			services := rule.GetServices()
			paths := rule.GetPaths()
			methods := rule.GetMethods()
			constraints := rule.GetConstraints()
			glog.Infof("Checking rule: services %v, path %v, method %v, constraints %v", services, paths, methods, constraints)

			if stringMatch(serviceName, services) &&
				(paths == nil || stringMatch(path, paths)) &&
				stringMatch(method, methods) &&
				checkConstraints(extraAct, constraints) {
				eligibleRole = true
				break
			}
		}
		if !eligibleRole {
			glog.Infof("role %s is not eligible", rolename)
			continue
		}
		glog.Infof("role %s is eligible", rolename)
		bindings := roleInfo.bindings
		for _, binding := range bindings {
			subjects := binding.GetSubjects()
			for _, subject := range subjects {
				if checkSubject(user, groups, instSub, subject.GetProperties()) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Helper function to check if a string is in a list.
func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" {
			return true
		}
	}
	return false
}

// Helper function to check if a given string value
// is in a list of strings.
func valueInList(value interface{}, list []string) bool {
	switch value.(type) {
	case string:
		return stringMatch(value.(string), list)
	default:
		glog.Error("Invalid data type for contraints: only string is supported.")
	}
	return false
}

// Check if all constraints in a rule can be satisfied by the properties from the request.
func checkConstraints(properties map[string]interface{}, constraints []*cpb.AccessRule_Constraint) bool {
	for _, constraint := range constraints {
		foundMatch := false
		for pn, pv := range properties {
			if pn == constraint.GetName() && valueInList(pv, constraint.GetValue()) {
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
	switch a.(type) {
	case string:
		return a.(string) == b
	default:
		glog.Error("Invalid data type for contraints: only string is supported.")
	}
	return false
}

// Check if all properties defined for the subject are satisfied by the properties from the request.
func checkSubject(user string, groups string, properties map[string]interface{}, subject map[string]string) bool {
	glog.Infof("checking subjects: %s, %s, subject %v", user, groups, subject)
	for sn, sv := range subject {
		if sn == "user" && sv == user {
			continue
		}
		// Comment out since "groups" in Authorization template is not supported at the moment.
		// if sn == "group" && stringMatch(sv, groups) { continue }
		if properties[sn] == nil || !valueMatch(properties[sn], sv) {
			return false
		}
	}
	return true
}
