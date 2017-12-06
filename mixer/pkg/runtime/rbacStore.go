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
	"sync"
	"github.com/golang/glog"
	cpb "istio.io/istio/mixer/pkg/config/proto"
//	"istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/template/authorization"
)

type Rbac interface {
	CheckPermission(inst *authorization.Instance) (bool, error)
}

// Information about a ServiceRole
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

// RbacStore implements Rbac interface
type RbacStore struct {
	// All the roles organized per namespace.
	roles rolesMapByNamespace

	// RW lock to protect all roles information.
	rolesLock sync.RWMutex
}

// The single instance of RBAC store. It is initialized when runtime is initiated.
var RbacInstance RbacStore

func newRoleInfo(spec *cpb.ServiceRole) *roleInfo {
	return &roleInfo{
		info: spec,
	}
}

func (ri *roleInfo) setBinding(name string, spec *cpb.ServiceRoleBinding) {
	if ri.bindings == nil {
		ri.bindings = make(map[string]*cpb.ServiceRoleBinding)
	}
	ri.bindings[name] = spec
}

func (rs *RbacStore) getRoles() rolesMapByNamespace {
	var rolesMap rolesMapByNamespace
	rs.rolesLock.RLock()
	rolesMap = rs.roles
	rs.rolesLock.RUnlock()
	return rolesMap
}

func (rs *RbacStore) changeRoles(roles rolesMapByNamespace) {
	rs.rolesLock.Lock()
	rs.roles = roles
	rs.rolesLock.Unlock()
}

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

	rs.rolesLock.RLock()
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
	rs.rolesLock.RUnlock()
	return false, nil
}

func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" {
			return true
		}
	}
	return false
}

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
		if sn == "user" && sv == user { continue }
//		if sn == "group" && stringMatch(sv, groups) { continue }
		if properties[sn] == nil || !valueMatch(properties[sn], sv) {
			return false
		}
	}
	return true
}