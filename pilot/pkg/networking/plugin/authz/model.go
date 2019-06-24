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

package authz

import (
	"fmt"

	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	istio_rbac "istio.io/api/rbac/v1alpha1"

	"istio.io/istio/pilot/pkg/networking/plugin/authz/rbacfilter"

	"sort"

	"strings"
)

// Model includes a group of permission and principals defining the access control semantics. The
// Permissions specify a list of allowed actions, the Principals specify a list of allowed source
// identities. A request is allowed if it matches any of the permissions and any of the principals.
type Model struct {
	Permissions []Permission
	Principals  []Principal
}

type KeyValues map[string][]string

type Permission struct {
	Services    []string // For backward-compatible only.
	Hosts       []string
	NotHosts    []string
	Paths       []string
	NotPaths    []string
	Methods     []string
	NotMethods  []string
	Ports       []int32
	NotPorts    []int32
	Constraints []KeyValues
}

type Principal struct {
	User          string // For backward-compatible only.
	Names         []string
	NotNames      []string
	Group         string // For backward-compatible only.
	Groups        []string
	NotGroups     []string
	Namespaces    []string
	NotNamespaces []string
	IPs           []string
	NotIPs        []string
	Properties    []KeyValues
}

// NewModel constructs a Model from a single ServiceRole and a list of ServiceRoleBinding. The ServiceRole
// is converted to the permission and the ServiceRoleBinding is converted to the principal.
func NewModel(role *istio_rbac.ServiceRole, bindings []*istio_rbac.ServiceRoleBinding) *Model {
	m := &Model{}
	for _, accessRule := range role.Rules {
		var permission Permission
		permission.Services = accessRule.Services
		permission.Hosts = accessRule.Hosts
		permission.NotHosts = accessRule.NotHosts
		permission.Paths = accessRule.Paths
		permission.NotPaths = accessRule.NotPaths
		permission.Methods = accessRule.Methods
		permission.NotMethods = accessRule.NotMethods
		permission.Ports = accessRule.Ports
		permission.NotPorts = accessRule.NotPorts

		constraints := KeyValues{}
		for _, constraint := range accessRule.Constraints {
			constraints[constraint.Key] = constraint.Values
		}
		permission.Constraints = []KeyValues{constraints}

		m.Permissions = append(m.Permissions, permission)
	}

	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			var principal Principal
			principal.User = subject.User
			principal.Names = subject.Names
			principal.NotNames = subject.NotNames
			principal.Groups = subject.Groups
			principal.Group = subject.Group
			principal.NotGroups = subject.NotGroups
			principal.Namespaces = subject.Namespaces
			principal.NotNamespaces = subject.NotNamespaces
			principal.IPs = subject.Ips
			principal.NotIPs = subject.NotIps

			property := KeyValues{}
			for k, v := range subject.Properties {
				property[k] = []string{v}
			}
			principal.Properties = []KeyValues{property}

			m.Principals = append(m.Principals, principal)
		}
	}

	return m
}

// Generate generates the envoy RBAC filter policy based on the permission and principals specified
// in the model for the given service. This function only generates the policy if the constraints
// and properties specified in the model is matched with the given service. It also validates if the
// model is valid for TCP filter.
func (m *Model) Generate(service *serviceMetadata, forTCPFilter bool) *envoy_rbac.Policy {
	policy := &envoy_rbac.Policy{}
	for _, permission := range m.Permissions {
		if permission.Match(service) {
			p, err := permission.generate(forTCPFilter)
			if err != nil {
				rbacLog.Debugf("ignored HTTP permission for TCP service: %v", err)
				continue
			}

			policy.Permissions = append(policy.Permissions, p)
		}
	}
	if len(policy.Permissions) == 0 {
		rbacLog.Debugf("role skipped for no permission matched")
		return nil
	}

	for _, principal := range m.Principals {
		p, err := principal.generate(forTCPFilter)
		if err != nil {
			rbacLog.Debugf("ignored HTTP principal for TCP service: %v", err)
			continue
		}

		policy.Principals = append(policy.Principals, p)
	}
	if len(policy.Principals) == 0 {
		rbacLog.Debugf("role skipped for no principals found")
		return nil
	}
	return policy
}

// Match returns True if the calling service's attributes and/or labels match to the ServiceRole constraints.
func (permission *Permission) Match(service *serviceMetadata) bool {
	if permission == nil {
		return true
	}

	// Check if the service name is matched.
	if len(permission.Services) != 0 {
		if !stringMatch(service.name, permission.Services) {
			return false
		}
	}

	// Check if the constraints are matched.
	for _, constraint := range permission.Constraints {
		for key, values := range constraint {
			var constraintValue string
			var present bool
			switch {
			case strings.HasPrefix(key, attrDestLabel):
				label, err := extractNameInBrackets(strings.TrimPrefix(key, attrDestLabel))
				if err != nil {
					rbacLog.Errorf("ignored invalid %s: %v", attrDestLabel, err)
					continue
				}
				constraintValue, present = service.labels[label]
			case key == attrDestName || key == attrDestNamespace:
				constraintValue, present = service.attributes[key]
			default:
				continue
			}

			// The constraint is not matched if any of the follow condition is true:
			// a) the constraint is specified but not found in the serviceMetadata;
			// b) the constraint value is not matched to the actual value;
			if !present || !stringMatch(constraintValue, values) {
				return false
			}
		}
	}
	return true
}

// ValidateForTCP checks if the permission is valid for TCP filter. A permission is not valid for TCP
// filter if it includes any HTTP-only fields, e.g. hosts, paths, etc.
func (permission *Permission) ValidateForTCP(forTCP bool) error {
	if permission == nil || !forTCP {
		return nil
	}

	if len(permission.Hosts) != 0 {
		return fmt.Errorf("hosts(%v)", permission.Hosts)
	}
	if len(permission.NotHosts) != 0 {
		return fmt.Errorf("hosts(%v)", permission.NotHosts)
	}
	if len(permission.Paths) != 0 {
		return fmt.Errorf("paths(%v)", permission.Paths)
	}
	if len(permission.NotPaths) != 0 {
		return fmt.Errorf("not_paths(%v)", permission.NotPaths)
	}
	if len(permission.Methods) != 0 {
		return fmt.Errorf("methods(%v)", permission.Methods)
	}
	if len(permission.NotMethods) != 0 {
		return fmt.Errorf("not_methods(%v)", permission.NotMethods)
	}
	for _, constraint := range permission.Constraints {
		for k := range constraint {
			if strings.HasPrefix(k, attrRequestHeader) {
				return fmt.Errorf("constraint(%v)", constraint)
			}
		}
	}
	return nil
}

// ValidateForTCP checks if the principal is valid for TCP filter. A principal is not valid for TCP
// filter if it includes any HTTP-only fields, e.g. group, etc.
func (principal *Principal) ValidateForTCP(forTCP bool) error {
	if principal == nil || !forTCP {
		return nil
	}

	if principal.Group != "" {
		return fmt.Errorf("group(%v)", principal.Groups)
	}

	if len(principal.Groups) != 0 {
		return fmt.Errorf("groups(%v)", principal.Groups)
	}
	if len(principal.NotGroups) != 0 {
		return fmt.Errorf("not_groups(%v)", principal.NotGroups)
	}

	for _, p := range principal.Properties {
		for key := range p {
			switch key {
			case attrSrcIP, attrSrcNamespace, attrSrcPrincipal:
				continue
			default:
				return fmt.Errorf("property(%v)", p)
			}
		}
	}
	return nil
}

func (permission *Permission) generate(forTCPFilter bool) (*envoy_rbac.Permission, error) {
	if err := permission.ValidateForTCP(forTCPFilter); err != nil {
		return nil, err
	}
	pg := rbacfilter.PermissionGenerator{}

	if len(permission.Hosts) > 0 {
		permission := permissionForKeyValues(hostHeader, permission.Hosts)
		pg.Append(permission)
	}

	if len(permission.NotHosts) > 0 {
		permission := permissionForKeyValues(hostHeader, permission.NotHosts)
		pg.Append(rbacfilter.PermissionNot(permission))
	}

	if len(permission.Methods) > 0 {
		permission := permissionForKeyValues(methodHeader, permission.Methods)
		pg.Append(permission)
	}

	if len(permission.NotMethods) > 0 {
		permission := permissionForKeyValues(methodHeader, permission.NotMethods)
		pg.Append(rbacfilter.PermissionNot(permission))
	}

	if len(permission.Paths) > 0 {
		permission := permissionForKeyValues(pathHeader, permission.Paths)
		pg.Append(permission)
	}

	if len(permission.NotPaths) > 0 {
		permission := permissionForKeyValues(pathHeader, permission.NotPaths)
		pg.Append(rbacfilter.PermissionNot(permission))
	}

	if len(permission.Ports) > 0 {
		permission := permissionForKeyValues(attrDestPort, convertPortsToString(permission.Ports))
		pg.Append(permission)
	}

	if len(permission.NotPorts) > 0 {
		permission := permissionForKeyValues(attrDestPort, convertPortsToString(permission.NotPorts))
		pg.Append(rbacfilter.PermissionNot(permission))
	}

	if len(permission.Constraints) > 0 {
		// Constraints are matched with AND semantics, it's invalid if 2 constraints have the same
		// key and this should already be caught by validation.
		for _, constraint := range permission.Constraints {
			var keys []string
			for key := range constraint {
				keys = append(keys, key)
			}
			sort.Strings(keys)

			for _, k := range keys {
				permission := permissionForKeyValues(k, constraint[k])
				pg.Append(permission)
			}
		}
	}

	if pg.IsEmpty() {
		// None of above permission satisfied means the permission applies to all paths/methods/constraints.
		pg.Append(rbacfilter.PermissionAny(true))
	}

	return pg.AndPermissions(), nil
}

func (principal *Principal) generate(forTCPFilter bool) (*envoy_rbac.Principal, error) {
	if err := principal.ValidateForTCP(forTCPFilter); err != nil {
		return nil, err
	}

	pg := rbacfilter.PrincipalGenerator{}
	if principal.User != "" {
		principal := principalForKeyValue(attrSrcPrincipal, principal.User, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.Names) > 0 {
		principal := principalForKeyValues(attrSrcPrincipal, principal.Names, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.NotNames) > 0 {
		principal := principalForKeyValues(attrSrcPrincipal, principal.NotNames, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(principal))
	}

	if principal.Group != "" {
		principal := principalForKeyValue(attrRequestClaimGroups, principal.Group, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.Groups) > 0 {
		// TODO: Validate attrRequestClaimGroups and principal.Groups are not used at the same time.
		principal := principalForKeyValues(attrRequestClaimGroups, principal.Groups, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.NotGroups) > 0 {
		principal := principalForKeyValues(attrRequestClaimGroups, principal.NotGroups, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(principal))
	}

	if len(principal.Namespaces) > 0 {
		principal := principalForKeyValues(attrSrcNamespace, principal.Namespaces, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.NotNamespaces) > 0 {
		principal := principalForKeyValues(attrSrcNamespace, principal.NotNamespaces, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(principal))
	}

	if len(principal.IPs) > 0 {
		principal := principalForKeyValues(attrSrcIP, principal.IPs, forTCPFilter)
		pg.Append(principal)
	}

	if len(principal.NotIPs) > 0 {
		principal := principalForKeyValues(attrSrcIP, principal.NotIPs, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(principal))
	}

	for _, p := range principal.Properties {
		// Use a separate key list to make sure the map iteration order is stable, so that the generated
		// config is stable.
		var keys []string
		for key := range p {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, k := range keys {
			// TODO: Validate attrSrcPrincipal and principal.Names are not used at the same time.
			principal := principalForKeyValues(k, p[k], forTCPFilter)
			if len(p[k]) == 1 {
				// FIXME: Temporary hack to avoid changing unit tests during code refactor. Remove once
				// we finish the code refactor with new unit tests.
				principal = principalForKeyValue(k, p[k][0], forTCPFilter)
			}
			pg.Append(principal)
		}
	}

	if pg.IsEmpty() {
		// None of above principal satisfied means nobody has the permission.
		principal := rbacfilter.PrincipalNot(rbacfilter.PrincipalAny(true))
		pg.Append(principal)
	}

	return pg.AndPrincipals(), nil
}
