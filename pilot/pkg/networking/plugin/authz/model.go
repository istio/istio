package authz

import (
	"fmt"

	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	istio_rbac "istio.io/api/rbac/v1alpha1"

	"istio.io/istio/pilot/pkg/networking/plugin/authz/rbacfilter"

	"sort"

	"strings"
)

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
		rbacLog.Debugf("role skipped for no rule matched")
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
		rule := permissionForKeyValues(hostHeader, permission.Hosts)
		pg.Append(rule)
	}

	if len(permission.NotHosts) > 0 {
		rule := permissionForKeyValues(hostHeader, permission.NotHosts)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(permission.Methods) > 0 {
		rule := permissionForKeyValues(methodHeader, permission.Methods)
		pg.Append(rule)
	}

	if len(permission.NotMethods) > 0 {
		rule := permissionForKeyValues(methodHeader, permission.NotMethods)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(permission.Paths) > 0 {
		rule := permissionForKeyValues(pathHeader, permission.Paths)
		pg.Append(rule)
	}

	if len(permission.NotPaths) > 0 {
		rule := permissionForKeyValues(pathHeader, permission.NotPaths)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(permission.Ports) > 0 {
		rule := permissionForKeyValues(attrDestPort, convertPortsToString(permission.Ports))
		pg.Append(rule)
	}

	if len(permission.NotPorts) > 0 {
		rule := permissionForKeyValues(attrDestPort, convertPortsToString(permission.NotPorts))
		pg.Append(rbacfilter.PermissionNot(rule))
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
				rule := permissionForKeyValues(k, constraint[k])
				pg.Append(rule)
			}
		}
	}

	if pg.IsEmpty() {
		// None of above rule satisfied means the permission applies to all paths/methods/constraints.
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
		id := principalForKeyValue(attrSrcPrincipal, principal.User, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.Names) > 0 {
		id := principalForKeyValues(attrSrcPrincipal, principal.Names, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.NotNames) > 0 {
		id := principalForKeyValues(attrSrcPrincipal, principal.NotNames, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if principal.Group != "" {
		id := principalForKeyValue(attrRequestClaimGroups, principal.Group, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.Groups) > 0 {
		// TODO: Validate attrRequestClaimGroups and principal.Groups are not used at the same time.
		id := principalForKeyValues(attrRequestClaimGroups, principal.Groups, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.NotGroups) > 0 {
		id := principalForKeyValues(attrRequestClaimGroups, principal.NotGroups, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(principal.Namespaces) > 0 {
		id := principalForKeyValues(attrSrcNamespace, principal.Namespaces, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.NotNamespaces) > 0 {
		id := principalForKeyValues(attrSrcNamespace, principal.NotNamespaces, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(principal.IPs) > 0 {
		id := principalForKeyValues(attrSrcIP, principal.IPs, forTCPFilter)
		pg.Append(id)
	}

	if len(principal.NotIPs) > 0 {
		id := principalForKeyValues(attrSrcIP, principal.NotIPs, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
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
			id := principalForKeyValues(k, p[k], forTCPFilter)
			if len(p[k]) == 1 {
				// FIXME: Temporary hack to avoid changing unit tests during code refactor. Remove once
				// we finish the code refactor with new unit tests.
				id = principalForKeyValue(k, p[k][0], forTCPFilter)
			}
			pg.Append(id)
		}
	}

	if pg.IsEmpty() {
		// None of above principal satisfied means nobody has the permission.
		id := rbacfilter.PrincipalNot(rbacfilter.PrincipalAny(true))
		pg.Append(id)
	}

	return pg.AndPrincipals(), nil
}
