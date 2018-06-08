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

// package authz converts Istio RBAC (role-based-access-control) policies (ServiceRole and ServiceRoleBinding)
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
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

const (
	// RbacFilterName is the name of the RBAC filter in envoy.
	RbacFilterName = "envoy.filters.http.rbac"

	// RbacConfigName is the name of the RbacConfig custom resource that controls the RBAC behavior.
	RbacConfigName = "rbac-config"

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
	if in.Node.Type != model.Sidecar || in.ListenerType != plugin.ListenerTypeHTTP {
		return nil
	}
	if !isRbacEnabled(in.Env.IstioConfigStore) {
		return nil
	}

	filter := buildHTTPFilter(in.ServiceInstance.Service.Hostname, in.Env.IstioConfigStore)
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

func isRbacEnabled(store model.IstioConfigStore) bool {
	var configProto *rbacproto.RbacConfig
	config := store.RbacConfig(RbacConfigName, kube.IstioNamespace)
	if config != nil {
		configProto = config.Spec.(*rbacproto.RbacConfig)
	}
	if configProto == nil {
		log.Debugf("rbac plugin disabled: No RbacConfig")
		return false
	}
	// TODO(yangminzhu): Supports ON_WITH_INCLUSION and ON_WITH_EXCLUSION.
	if configProto.Mode == rbacproto.RbacConfig_ON {
		return true
	}

	log.Debugf("rbac plugin disabled by RbacConfig: %v", *configProto)
	return false
}

// buildHTTPFilter builds the RBAC http filter that enforces the access control to the specified
// service which is co-located with the sidecar proxy.
func buildHTTPFilter(hostName model.Hostname, store model.IstioConfigStore) *http_conn.HttpFilter {
	namespace := hostName.Namespace()
	roles := store.ServiceRoles(namespace)
	if roles == nil {
		log.Debugf("no service role found for %s in namespace %s", hostName, namespace)
	}
	bindings := store.ServiceRoleBindings(namespace)
	if bindings == nil {
		log.Debugf("no service role binding found for %s in namespace %s", hostName, namespace)
	}
	config := convertRbacRulesToFilterConfig(hostName.String(), roles, bindings)
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

	for _, role := range roles {
		// Constructs the policy for each ServiceRole.
		var policy *policyproto.Policy
		var principals []*policyproto.Principal
		log.Debugf("checking role %v for service %v", role.Name, service)
		for i, rule := range role.Spec.(*rbacproto.ServiceRole).Rules {
			//TODO(yangminzhu): Also check the destination related properties.
			if stringMatch(service, rule.Services) {
				// Generate the policy if the service is matched to the services specified in ServiceRole.
				log.Debugf("role %v matched by AccessRule %d", role.Name, i)
				if policy == nil {
					if principals == nil {
						principals = convertToPrincipals(roleToBinding[role.Name])
					}
					policy = &policyproto.Policy{
						Permissions: []*policyproto.Permission{},
						Principals:  principals,
					}
				}
				policy.Permissions = append(policy.Permissions, convertToPermission(rule))
			}
		}

		if policy != nil {
			log.Debugf("role %v generated policy %v", role.Name, *policy)
			rbac.Policies[role.Name] = policy
		}
	}

	return &rbacconfig.RBAC{Rules: rbac}
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

// convertToPrincipals converts a list of subjects to principals.
func convertToPrincipals(bindings []*rbacproto.ServiceRoleBinding) []*policyproto.Principal {
	principals := make([]*policyproto.Principal, 0)
	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			principals = append(principals, convertToPrincipal(subject))
		}
	}
	return principals
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
			if cidr, err := convertToCidr(v); err != nil {
				return nil, err
			} else {
				return &policyproto.Permission{
					Rule: &policyproto.Permission_DestinationIp{DestinationIp: cidr},
				}, nil
			}
		}
	case key == destinationPort:
		converter = func(v string) (*policyproto.Permission, error) {
			if port, err := convertToPort(v); err != nil {
				return nil, err
			} else {
				return &policyproto.Permission{
					Rule: &policyproto.Permission_DestinationPort{DestinationPort: port},
				}, nil
			}
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
