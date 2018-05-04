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

package authz

import (
	"fmt"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	// RbacFilterName is the name for the Rbac filter.
	RbacFilterName = "rbac-authz"
)

// Plugin implements Istio RBAC authz
type Plugin struct{}

// NewPlugin returns an instance of the authz plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.Sidecar {
		// The rbac filter only supports sidecar for now.
		return nil
	}

	for i := range mutable.Listener.FilterChains {
		// The rbac filter only supports HTTP listener for now.
		if in.ListenerType == plugin.ListenerTypeHTTP {
			serviceName := in.ServiceInstance.Service.Hostname
			if filter := buildHttpFilter(serviceName, in.Env.IstioConfigStore); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
		}
	}

	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

func buildHttpFilter(serviceName string, store model.IstioConfigStore) *http_conn.HttpFilter {
	split := strings.Split(serviceName, ".")
	if len(split) < 2 {
		log.Errorf("failed to extract namespace from service: %s", serviceName)
		return nil
	}
	namespace := split[1]

	roles, err := store.List(model.ServiceRole.Type, namespace)
	if err != nil {
		log.Errorf("failed to get ServiceRoles in namespace %s: %v", namespace, err)
		return nil
	}

	bindings, err := store.List(model.ServiceRoleBinding.Type, namespace)
	if err != nil {
		log.Errorf("failed to get ServiceRoleBinding in namespace %s: %v", namespace, err)
		return nil
	}

	config, err := convertRbacRulesToFilterConfig(serviceName, roles, bindings)
	if err != nil {
		log.Errorf("failed to convert RBAC rules to filter config: %v", err)
		return nil
	}

	return &http_conn.HttpFilter{
		Name:   RbacFilterName,
		Config: util.MessageToStruct(config),
	}
}

func convertRbacRulesToFilterConfig(service string, roles []model.Config, bindings []model.Config) (*RBAC, error) {
	// roleToBinding maps ServiceRole name to a list of ServiceRoleBinding protos.
	roleToBinding := map[string][]*rbacproto.ServiceRoleBinding{}
	for _, binding := range bindings {
		proto := binding.Spec.(*rbacproto.ServiceRoleBinding)
		refName := proto.RoleRef.Name
		if refName == "" {
			return nil, fmt.Errorf("invalid RoleRef.Name in binding: %v", proto)
		}
		roleToBinding[refName] = append(roleToBinding[refName], proto)
	}

	rbac := &RBAC{
		// TODO(yangminzhu): Supports RBAC_DENY based on RbacConfig.
		Action:   RBAC_ALLOW,
		Policies: map[string]*Policy{},
	}

	// Constructs a policy for each ServiceRole.
	for _, role := range roles {
		policy := &Policy{
			Permissions: []*Permission{},
			Principals:  []*Principal{},
		}

		principal := convertToPrincipals(roleToBinding[role.Name])
		proto := role.Spec.(*rbacproto.ServiceRole)
		hasPolicy := false
		for _, rule := range proto.Rules {
			if stringMatch(service, rule.Services) {
				policy.Permissions = append(policy.Permissions, convertToPermission(rule))
				policy.Principals = principal
				hasPolicy = true
			}
		}

		if hasPolicy {
			rbac.Policies[role.Name] = policy
		}
	}

	return rbac, nil
}

// convertToPermission converts a single AccessRule to a Permission.
func convertToPermission(rule *rbacproto.AccessRule) *Permission {
	permission := &Permission{}

	if len(rule.Methods) > 0 {
		permission.Methods = make([]string, len(rule.Methods))
		copy(permission.Methods, rule.Methods)
	}

	if len(rule.Paths) > 0 {
		permission.Paths = make([]*StringMatch, 0)
		for _, path := range rule.Paths {
			permission.Paths = append(permission.Paths, convertToStringMatch(path))
		}
	}

	if len(rule.Constraints) > 0 {
		conditions := make([]*Permission_Condition, 0)
		for _, v := range rule.Constraints {
			conditions = append(conditions, &Permission_Condition{
				ConditionSpec: &Permission_Condition_Header{
					Header: &MapEntryMatch{
						Key:    v.Key,
						Values: convertToStringMatches(v.Values)},
				}})
		}
		permission.Conditions = conditions
	}

	return permission
}

func convertToStringMatches(list []string) []*StringMatch {
	matches := make([]*StringMatch, 0)
	for _, s := range list {
		matches = append(matches, convertToStringMatch(s))
	}
	return matches
}

// convertToStringMatch converts a string to a StringMatch, it supports four types of conversion:
// 1. Exact match, e.g. "abc" is converted to a simple exact match of "abc"
// 2. Suffix match, e.g. "*abc" is converted to a suffix match of "abc"
// 3. Prefix match, e.g. "abc* " is converted to a prefix match of "abc"
// 4. All match. i.e. "*" is converted to a regular expression match of "*"
func convertToStringMatch(s string) *StringMatch {
	s = strings.TrimSpace(s)
	switch {
	case s == "*":
		return &StringMatch{MatchPattern: &StringMatch_Regex{Regex: "*"}}
	case strings.HasPrefix(s, "*"):
		return &StringMatch{MatchPattern: &StringMatch_Suffix{Suffix: strings.TrimPrefix(s, "*")}}
	case strings.HasSuffix(s, "*"):
		return &StringMatch{MatchPattern: &StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "*")}}
	default:
		return &StringMatch{MatchPattern: &StringMatch_Simple{Simple: s}}
	}
}

// convertToPrincipals converts a list of subjects to principals.
func convertToPrincipals(bindings []*rbacproto.ServiceRoleBinding) []*Principal {
	principals := make([]*Principal, 0)
	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			principals = append(principals, convertToPrincipal(subject))
		}
	}
	return principals
}

// convertToPrincipal converts a single subject to principal.
func convertToPrincipal(subject *rbacproto.Subject) *Principal {
	principal := &Principal{}

	if subject.Group != "" {
		log.Errorf("group is not supported for now but set to %s.", subject.Group)
	}
	attributes := []*Principal_Attribute{}
	for k, v := range subject.Properties {
		attributes = append(attributes, &Principal_Attribute{
			AttributeSpec: &Principal_Attribute_Header{
				Header: &MapEntryMatch{
					Key:    k,
					Values: []*StringMatch{convertToStringMatch(v)}},
			}})
	}
	if len(attributes) > 0 {
		principal.Attributes = attributes
	}

	if subject.User != "" {
		principal.Authenticated = &Principal_Authenticated{
			Name: subject.User,
		}
	}

	return principal
}

// stringMatch checks if a string is in a list, it supports four types of string matches:
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

// prefixMatch checks if string "a" prefix matches "pattern".
func prefixMatch(a string, pattern string) bool {
	if !strings.HasSuffix(pattern, "*") {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(a, pattern)
}

// suffixMatch checks if string "a" prefix matches "pattern".
func suffixMatch(a string, pattern string) bool {
	if !strings.HasPrefix(pattern, "*") {
		return false
	}
	pattern = strings.TrimPrefix(pattern, "*")
	return strings.HasSuffix(a, pattern)
}
