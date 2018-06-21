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

// Package authz converts Istio RBAC (role-based-access-control) policies (ServiceRole and ServiceRoleBinding)
// to corresponding filter config that is used by the envoy RBAC filter to enforce access control to
// the service co-located with envoy.
// Currently the config is only generated for sidecar node on inbound HTTP listener. The generation
// is controlled by RbacConfig (a singleton custom resource defined in istio-system namespace). User
// could disable this by either deleting the RbacConfig or set the RbacConfig.mode to OFF.
// Note: This is still working in progress and by default no RbacConfig is created in the deployment
// of Istio which means this plugin doesn't generate any RBAC config by default.
package authz

import (
	"sort"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	rbacconfig "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	// RbacFilterName is the name of the RBAC filter in envoy.
	RbacFilterName = "envoy.filters.http.rbac"

	sourceIP        = "source.ip"
	sourceService   = "source.service"
	destinationIP   = "destination.ip"
	destinationPort = "destination.port"
	userHeader      = ":user"
	methodHeader    = ":method"
	pathHeader      = ":path"
	//TODO(yangminzhu): Update the key after the final header name of service is decided.
	serviceHeader       = ":service"
	requestHeaderPrefix = "request.header["
	requestHeaderSuffix = "]"
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
	// Only supports sidecar proxy of HTTP listener for now.
	if in.Node.Type != model.Sidecar || in.ListenerProtocol != plugin.ListenerProtocolHTTP {
		return nil
	}
	svc := in.ServiceInstance.Service.Hostname
	attr, err := in.Env.GetServiceAttributes(svc)
	if attr == nil || err != nil {
		log.Errorf("rbac plugin disabled: invalid service %s: %v", svc, err)
		return nil
	}

	if !isRbacEnabled(svc.String(), attr.Namespace, in.Env.IstioConfigStore) {
		return nil
	}

	filter := buildHTTPFilter(svc.String(), attr.Namespace, in.Env.IstioConfigStore)
	if filter != nil {
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
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

// isServiceInList checks if a given service of namespace is found in the RbacConfig target list.
func isServiceInList(svc string, namespace string, li *rbacproto.RbacConfig_Target) bool {
	for _, ns := range li.Namespaces {
		if namespace == ns {
			return true
		}
	}
	for _, service := range li.Services {
		if service == svc {
			return true
		}
	}
	return false
}

func isRbacEnabled(svc string, ns string, store model.IstioConfigStore) bool {
	var configProto *rbacproto.RbacConfig
	config := store.RbacConfig(model.DefaultRbacConfigName)
	if config != nil {
		configProto = config.Spec.(*rbacproto.RbacConfig)
	}
	if configProto == nil {
		log.Debugf("rbac plugin disabled: no RbacConfig found")
		return false
	}

	switch configProto.Mode {
	case rbacproto.RbacConfig_ON:
		return true
	case rbacproto.RbacConfig_ON_WITH_INCLUSION:
		return isServiceInList(svc, ns, configProto.Inclusion)
	case rbacproto.RbacConfig_ON_WITH_EXCLUSION:
		return !isServiceInList(svc, ns, configProto.Exclusion)
	default:
		log.Debugf("rbac plugin disabled by RbacConfig: %v", *configProto)
		return false
	}
}

// buildHTTPFilter builds the RBAC http filter that enforces the access control to the specified
// service which is co-located with the sidecar proxy.
func buildHTTPFilter(svc string, namespace string, store model.IstioConfigStore) *http_conn.HttpFilter {
	roles := store.ServiceRoles(namespace)
	if roles == nil {
		log.Debugf("no service role found for %s in namespace %s", svc, namespace)
	}
	bindings := store.ServiceRoleBindings(namespace)
	if bindings == nil {
		log.Debugf("no service role binding found for %s in namespace %s", svc, namespace)
	}
	config := convertRbacRulesToFilterConfig(svc, roles, bindings)
	return &http_conn.HttpFilter{
		Name:   RbacFilterName,
		Config: util.MessageToStruct(config),
	}
}

// convertRbacRulesToFilterConfig converts the current RBAC rules (ServiceRole and ServiceRoleBindings)
// in service mesh to the corresponding proxy config for the specified service. The generated proxy config
// will be consumed by envoy RBAC filter to enforce access control on the specified service.
func convertRbacRulesToFilterConfig(service string, roles []model.Config, bindings []model.Config) *rbacconfig.RBAC {
	// roleToBinding maps ServiceRole name to a list of ServiceRoleBindings.
	roleToBinding := map[string][]*rbacproto.ServiceRoleBinding{}
	for _, binding := range bindings {
		bindingProto := binding.Spec.(*rbacproto.ServiceRoleBinding)
		roleName := bindingProto.RoleRef.Name
		if roleName == "" {
			log.Errorf("ignored invalid binding with empty RoleRef.Name: %v", *bindingProto)
			continue
		}
		roleToBinding[roleName] = append(roleToBinding[roleName], bindingProto)
	}

	rbac := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}

	permissiveRbac := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}

	for _, role := range roles {
		enforcedPrincipals, permissivePrincipals := convertToPrincipals(roleToBinding[role.Name])
		if len(enforcedPrincipals) == 0 && len(permissivePrincipals) == 0 {
			continue
		}

		log.Debugf("checking role %v for service %v", role.Name, service)
		permissions := make([]*policyproto.Permission, 0)
		for i, rule := range role.Spec.(*rbacproto.ServiceRole).Rules {
			//TODO(yangminzhu): Also check the destination related properties.
			if stringMatch(service, rule.Services) {
				// Generate the policy if the service is matched to the services specified in ServiceRole.
				log.Debugf("role %v matched by AccessRule %d", role.Name, i)
				permissions = append(permissions, convertToPermission(rule))
			}
		}
		if len(permissions) == 0 {
			log.Debugf("role %v skipped for no permissions found", role.Name)
			continue
		}

		if len(enforcedPrincipals) != 0 {
			rbac.Policies[role.Name] = &policyproto.Policy{
				Permissions: permissions,
				Principals:  enforcedPrincipals,
			}
			log.Debugf("role %v generated enforced policy: %v", role.Name, *rbac.Policies[role.Name])
		}

		if len(permissivePrincipals) != 0 {
			permissiveRbac.Policies[role.Name] = &policyproto.Policy{
				Permissions: permissions,
				Principals:  permissivePrincipals,
			}
			log.Debugf("role %v generated permissive policy: %v", role.Name, *permissiveRbac.Policies[role.Name])
		}
	}

	return &rbacconfig.RBAC{
		Rules:       rbac,
		ShadowRules: permissiveRbac}
}

// convertToPermission converts a single AccessRule to a Permission.
func convertToPermission(rule *rbacproto.AccessRule) *policyproto.Permission {
	rules := &policyproto.Permission_AndRules{
		AndRules: &policyproto.Permission_Set{
			Rules: make([]*policyproto.Permission, 0),
		},
	}

	if len(rule.Methods) > 0 {
		methodRule := permissionForKeyValues(methodHeader, rule.Methods)
		if methodRule != nil {
			rules.AndRules.Rules = append(rules.AndRules.Rules, methodRule)
		}
	}

	if len(rule.Paths) > 0 {
		pathRule := permissionForKeyValues(pathHeader, rule.Paths)
		if pathRule != nil {
			rules.AndRules.Rules = append(rules.AndRules.Rules, pathRule)
		}
	}

	if len(rule.Constraints) > 0 {
		// Constraint rule is matched with AND semantics, it's invalid if 2 constraints have the same
		// key and this should already be caught in validation stage.
		for _, constraint := range rule.Constraints {
			p := permissionForKeyValues(constraint.Key, constraint.Values)
			if p != nil {
				rules.AndRules.Rules = append(rules.AndRules.Rules, p)
			}
		}
	}

	return &policyproto.Permission{Rule: rules}
}

// convertToPrincipals converts subjects to two lists of principals, one from enforced mode ServiceBindings,
// and the other from permissive mode ServiceBindings.
func convertToPrincipals(bindings []*rbacproto.ServiceRoleBinding) ([]*policyproto.Principal, []*policyproto.Principal) {
	enforcedPrincipals := make([]*policyproto.Principal, 0)
	permissivePrincipals := make([]*policyproto.Principal, 0)

	for _, binding := range bindings {
		if binding.Mode == rbacproto.EnforcementMode_ENFORCED {
			for _, subject := range binding.Subjects {
				enforcedPrincipals = append(enforcedPrincipals, convertToPrincipal(subject))
			}
		} else {
			for _, subject := range binding.Subjects {
				permissivePrincipals = append(permissivePrincipals, convertToPrincipal(subject))
			}
		}
	}

	return enforcedPrincipals, permissivePrincipals
}

// convertToPrincipal converts a single subject to principal.
func convertToPrincipal(subject *rbacproto.Subject) *policyproto.Principal {
	ids := &policyproto.Principal_AndIds{
		AndIds: &policyproto.Principal_Set{
			Ids: make([]*policyproto.Principal, 0),
		},
	}

	if subject.User != "" {
		id := principalForKeyValue(userHeader, subject.User)
		if id != nil {
			ids.AndIds.Ids = append(ids.AndIds.Ids, id)
		}
	}

	if subject.Group != "" {
		log.Errorf("ignored Subject.group %s, not implemented", subject.Group)
	}

	if len(subject.Properties) != 0 {
		// Use a separate key list to make sure the map iteration order is stable, so that the generated
		// config is stable.
		var keys []string
		for k := range subject.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v, _ := subject.Properties[k]
			id := principalForKeyValue(k, v)
			if id != nil {
				ids.AndIds.Ids = append(ids.AndIds.Ids, id)
			}
		}
	}

	return &policyproto.Principal{Identifier: ids}
}

func permissionForKeyValues(key string, values []string) *policyproto.Permission {
	var converter func(string) (*policyproto.Permission, error)
	switch {
	case key == destinationIP:
		converter = func(v string) (*policyproto.Permission, error) {
			cidr, err := convertToCidr(v)
			if err != nil {
				return nil, err
			}
			return &policyproto.Permission{
				Rule: &policyproto.Permission_DestinationIp{DestinationIp: cidr},
			}, nil
		}
	case key == destinationPort:
		converter = func(v string) (*policyproto.Permission, error) {
			port, err := convertToPort(v)
			if err != nil {
				return nil, err
			}
			return &policyproto.Permission{
				Rule: &policyproto.Permission_DestinationPort{DestinationPort: port},
			}, nil
		}
	case key == pathHeader || key == methodHeader:
		converter = func(v string) (*policyproto.Permission, error) {
			return &policyproto.Permission{
				Rule: &policyproto.Permission_Header{
					Header: convertToHeaderMatcher(key, v),
				},
			}, nil
		}
	case strings.HasPrefix(key, requestHeaderPrefix) && strings.HasSuffix(key, requestHeaderSuffix):
		header := strings.TrimSuffix(strings.TrimPrefix(key, requestHeaderPrefix), requestHeaderSuffix)
		converter = func(v string) (*policyproto.Permission, error) {
			return &policyproto.Permission{
				Rule: &policyproto.Permission_Header{
					Header: convertToHeaderMatcher(header, v),
				},
			}, nil
		}
	default:
		log.Errorf("ignored unsupported constraint key: %s", key)
		return nil
	}

	orRules := &policyproto.Permission_OrRules{
		OrRules: &policyproto.Permission_Set{
			Rules: make([]*policyproto.Permission, 0),
		},
	}
	for _, v := range values {
		if p, err := converter(v); err != nil {
			log.Errorf("ignored invalid constraint value: %v", err)
		} else {
			orRules.OrRules.Rules = append(orRules.OrRules.Rules, p)
		}
	}

	return &policyproto.Permission{Rule: orRules}
}

func principalForKeyValue(key, value string) *policyproto.Principal {
	switch {
	case key == userHeader:
		return &policyproto.Principal{
			Identifier: &policyproto.Principal_Authenticated_{
				Authenticated: &policyproto.Principal_Authenticated{Name: value}},
		}
	case key == sourceIP:
		cidr, err := convertToCidr(value)
		if err != nil {
			log.Errorf("ignored invalid source ip value: %v", err)
			return nil
		}
		return &policyproto.Principal{Identifier: &policyproto.Principal_SourceIp{SourceIp: cidr}}
	case key == sourceService:
		return &policyproto.Principal{
			Identifier: &policyproto.Principal_Header{
				Header: convertToHeaderMatcher(serviceHeader, value),
			},
		}
	case strings.HasPrefix(key, requestHeaderPrefix) && strings.HasSuffix(key, requestHeaderSuffix):
		header := strings.TrimSuffix(strings.TrimPrefix(key, requestHeaderPrefix), requestHeaderSuffix)
		return &policyproto.Principal{
			Identifier: &policyproto.Principal_Header{
				Header: convertToHeaderMatcher(header, value),
			},
		}
	default:
		log.Errorf("ignored unsupported property key: %s", key)
		return nil
	}
}
