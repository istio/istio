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

package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	"istio.io/istio/pilot/pkg/security/authz/model/matcher"
)

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

// Match returns True if the calling service's attributes and/or labels match to the ServiceRole constraints.
func (permission *Permission) Match(service *ServiceMetadata) bool {
	if permission == nil {
		return true
	}

	// Check if the service name is matched.
	if len(permission.Services) != 0 {
		if !stringMatch(service.Name, permission.Services) {
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
				constraintValue, present = service.Labels[label]
			case key == attrDestName || key == attrDestNamespace:
				constraintValue, present = service.Attributes[key]
			default:
				continue
			}

			// The constraint is not matched if any of the follow condition is true:
			// a) the constraint is specified but not found in the ServiceMetadata;
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

func (permission *Permission) Generate(forTCPFilter bool) (*envoy_rbac.Permission, error) {
	if permission == nil {
		return nil, nil
	}

	if err := permission.ValidateForTCP(forTCPFilter); err != nil {
		return nil, err
	}
	pg := permissionGenerator{}

	if len(permission.Hosts) > 0 {
		permission := permissionForKeyValues(hostHeader, permission.Hosts)
		pg.append(permission)
	}

	if len(permission.NotHosts) > 0 {
		permission := permissionForKeyValues(hostHeader, permission.NotHosts)
		pg.append(permissionNot(permission))
	}

	if len(permission.Methods) > 0 {
		permission := permissionForKeyValues(methodHeader, permission.Methods)
		pg.append(permission)
	}

	if len(permission.NotMethods) > 0 {
		permission := permissionForKeyValues(methodHeader, permission.NotMethods)
		pg.append(permissionNot(permission))
	}

	if len(permission.Paths) > 0 {
		permission := permissionForKeyValues(pathHeader, permission.Paths)
		pg.append(permission)
	}

	if len(permission.NotPaths) > 0 {
		permission := permissionForKeyValues(pathHeader, permission.NotPaths)
		pg.append(permissionNot(permission))
	}

	if len(permission.Ports) > 0 {
		permission := permissionForKeyValues(attrDestPort, convertPortsToString(permission.Ports))
		pg.append(permission)
	}

	if len(permission.NotPorts) > 0 {
		permission := permissionForKeyValues(attrDestPort, convertPortsToString(permission.NotPorts))
		pg.append(permissionNot(permission))
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
				pg.append(permission)
			}
		}
	}

	if pg.isEmpty() {
		// None of above permission satisfied means the permission applies to all paths/methods/constraints.
		pg.append(permissionAny(true))
	}

	return pg.andPermissions(), nil
}

// permissionForKeyValues converts a key-values pair to an envoy RBAC permission. The key specify the
// type of the permission (e.g. destination IP, header, SNI, etc.), the values specify the allowed
// value of the key, multiple values are ORed together.
func permissionForKeyValues(key string, values []string) *envoy_rbac.Permission {
	var converter func(string) (*envoy_rbac.Permission, error)
	switch {
	case key == attrDestIP:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			cidr, err := matcher.CidrRange(v)
			if err != nil {
				return nil, err
			}
			return permissionDestinationIP(cidr), nil
		}
	case key == attrDestPort:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			portValue, err := convertToPort(v)
			if err != nil {
				return nil, err
			}
			return permissionDestinationPort(portValue), nil
		}
	case key == pathHeader || key == methodHeader || key == hostHeader:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.HeaderMatcher(key, v)
			return permissionHeader(m), nil
		}
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.HeaderMatcher(header, v)
			return permissionHeader(m), nil
		}
	case key == attrConnSNI:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.StringMatcher(v)
			return permissionRequestedServerName(m), nil
		}
	case strings.HasPrefix(key, "experimental.envoy.filters.") && isKeyBinary(key):
		// Split key of format experimental.envoy.filters.a.b[c] to [envoy.filters.a.b, c].
		parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)
		converter = func(v string) (*envoy_rbac.Permission, error) {
			// If value is of format [v], create a list matcher.
			// Else, if value is of format v, create a string matcher.
			var m *envoy_matcher.MetadataMatcher
			if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
				m = matcher.MetadataListMatcher(parts[0], parts[1:], strings.Trim(v, "[]"))
			} else {
				m = matcher.MetadataStringMatcher(parts[0], parts[1], matcher.StringMatcher(v))
			}
			return permissionMetadata(m), nil
		}
	default:
		if !found(key, []string{attrDestName, attrDestNamespace, attrDestUser}) &&
			!strings.HasPrefix(key, attrDestLabel) {
			// The attribute is neither matched here nor in previous stage, this means it's something we
			// don't understand, most likely a user typo.
			rbacLog.Errorf("ignored unsupported constraint: %s", key)
		}
		return nil
	}

	pg := permissionGenerator{}
	for _, v := range values {
		if permission, err := converter(v); err != nil {
			rbacLog.Errorf("ignored invalid constraint value: %v", err)
		} else {
			pg.append(permission)
		}
	}
	return pg.orPermissions()
}

type permissionGenerator struct {
	permissions []*envoy_rbac.Permission
}

func (pg *permissionGenerator) isEmpty() bool {
	return len(pg.permissions) == 0
}

func (pg *permissionGenerator) append(permission *envoy_rbac.Permission) {
	if permission == nil {
		return
	}
	pg.permissions = append(pg.permissions, permission)
}

func (pg *permissionGenerator) andPermissions() *envoy_rbac.Permission {
	if pg.isEmpty() {
		return nil
	}

	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_AndRules{
			AndRules: &envoy_rbac.Permission_Set{
				Rules: pg.permissions,
			},
		},
	}
}

func (pg *permissionGenerator) orPermissions() *envoy_rbac.Permission {
	if pg.isEmpty() {
		return nil
	}

	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_OrRules{
			OrRules: &envoy_rbac.Permission_Set{
				Rules: pg.permissions,
			},
		},
	}
}

func permissionAny(any bool) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_Any{
			Any: any,
		},
	}
}

func permissionNot(permission *envoy_rbac.Permission) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_NotRule{
			NotRule: permission,
		},
	}
}

func permissionDestinationIP(cidr *core.CidrRange) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_DestinationIp{
			DestinationIp: cidr,
		},
	}
}

func permissionDestinationPort(port uint32) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_DestinationPort{
			DestinationPort: port,
		},
	}
}

func permissionRequestedServerName(name *envoy_matcher.StringMatcher) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_RequestedServerName{
			RequestedServerName: name,
		},
	}
}

func permissionMetadata(metadata *envoy_matcher.MetadataMatcher) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_Metadata{
			Metadata: metadata,
		},
	}
}

func permissionHeader(header *route.HeaderMatcher) *envoy_rbac.Permission {
	return &envoy_rbac.Permission{
		Rule: &envoy_rbac.Permission_Header{
			Header: header,
		},
	}
}
