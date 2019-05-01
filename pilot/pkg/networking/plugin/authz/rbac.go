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
// Currently the config is only generated for sidecar node on inbound HTTP/TCP listener. The generation
// is controlled by RbacConfig (a singleton custom resource with cluster scope). User could disable
// this plugin by either deleting the ClusterRbacConfig or set the ClusterRbacConfig.mode to OFF.
// Note: ClusterRbacConfig is not created with default istio installation which means this plugin doesn't
// generate any RBAC config by default.
package authz

import (
	"fmt"
	"sort"
	"strings"

	"istio.io/istio/pkg/spiffe"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	network_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/authz/matcher"
	"istio.io/istio/pilot/pkg/networking/plugin/authz/rbacfilter"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_v1alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	istiolog "istio.io/istio/pkg/log"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

const (
	// rbacHTTPFilterName is the name of the RBAC http filter in envoy.
	rbacHTTPFilterName = "envoy.filters.http.rbac"

	// rbacTCPFilterName is the name of the RBAC network filter in envoy.
	rbacTCPFilterName       = "envoy.filters.network.rbac"
	rbacTCPFilterStatPrefix = "tcp."

	// attributes that could be used in both ServiceRoleBinding and ServiceRole.
	attrRequestHeader = "request.headers" // header name is surrounded by brackets, e.g. "request.headers[User-Agent]".

	// attributes that could be used in a ServiceRoleBinding property.
	attrSrcIP        = "source.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrSrcNamespace = "source.namespace" // e.g. "default".
	// TODO(pitlv2109): Since attrSrcUser will be deprecated, maybe remove this and use attrSrcPrincipal consistently everywhere?
	attrSrcUser            = "source.user"                 // source identity, e.g. "cluster.local/ns/default/sa/productpage".
	attrSrcPrincipal       = "source.principal"            // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal   = "request.auth.principal"      // authenticated principal of the request.
	attrRequestAudiences   = "request.auth.audiences"      // intended audience(s) for this authentication information.
	attrRequestPresenter   = "request.auth.presenter"      // authorized presenter of the credential.
	attrRequestClaims      = "request.auth.claims"         // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".
	attrRequestClaimGroups = "request.auth.claims[groups]" // groups claim.

	// reserved string values in names and not_names in ServiceRoleBinding.
	// This prevents ambiguity when the user defines "*" for names or not_names.
	allUsers              = "allUsers"              // Allow all users, both authenticated and unauthenticated.
	allAuthenticatedUsers = "allAuthenticatedUsers" // Allow all authenticated users.

	// attributes that could be used in a ServiceRole constraint.
	attrDestIP        = "destination.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort      = "destination.port"      // must be in the range [0, 65535].
	attrDestLabel     = "destination.labels"    // label name is surrounded by brackets, e.g. "destination.labels[version]".
	attrDestName      = "destination.name"      // short service name, e.g. "productpage".
	attrDestNamespace = "destination.namespace" // e.g. "default".
	attrDestUser      = "destination.user"      // service account, e.g. "bookinfo-productpage".
	attrConnSNI       = "connection.sni"        // server name indication, e.g. "www.example.com".

	// Envoy config attributes for ServiceRole rules.
	methodHeader = ":method"
	pathHeader   = ":path"
	hostHeader   = ":authority"
	portKey      = "port"
)

// serviceMetadata is a collection of different kind of information about a service.
type serviceMetadata struct {
	name       string            // full qualified service name, e.g. "productpage.default.svc.cluster.local
	labels     map[string]string // labels of the service instance
	attributes map[string]string // additional attributes of the service
}

type rbacOption struct {
	authzPolicies        *model.AuthorizationPolicies
	forTCPFilter         bool // The generated config is to be used by the Envoy network filter when true.
	globalPermissiveMode bool // True if global RBAC config is in permissive mode.
}

func createServiceMetadata(attr *model.ServiceAttributes, in *model.ServiceInstance) *serviceMetadata {
	if attr.Namespace == "" {
		rbacLog.Errorf("no namespace for service %v", in.Service.Hostname)
		return nil
	}

	return &serviceMetadata{
		name:   string(in.Service.Hostname),
		labels: in.Labels,
		attributes: map[string]string{
			attrDestName:      attr.Name,
			attrDestNamespace: attr.Namespace,
			attrDestUser:      extractActualServiceAccount(in.ServiceAccount),
		},
	}
}

// attributesEnforcedInPlugin returns true if the given attribute should be enforced in the plugin.
// This is because we already have enough information to do this in the plugin.
func attributesEnforcedInPlugin(attr string) bool {
	switch attr {
	case attrDestName, attrDestNamespace, attrDestUser:
		return true
	}
	return strings.HasPrefix(attr, attrDestLabel)
}

// match checks if the service is matched to the given rbac access rule.
// It returns true if the service is matched to the service name and constraints specified in the
// access rule.
func (service serviceMetadata) match(rule *rbacproto.AccessRule) bool {
	if rule == nil {
		return true
	}

	// Check if the service name is matched.
	if !stringMatch(service.name, rule.Services) {
		return false
	}

	// Check if the constraints are matched.
	return service.areConstraintsMatched(rule)
}

// areConstraintsMatched returns True if the calling service's attributes and/or labels match to
// the ServiceRole constraints.
func (service serviceMetadata) areConstraintsMatched(rule *rbacproto.AccessRule) bool {
	for _, constraint := range rule.Constraints {
		if !attributesEnforcedInPlugin(constraint.Key) {
			continue
		}

		var actualValue string
		var present bool
		if strings.HasPrefix(constraint.Key, attrDestLabel) {
			consLabel, err := extractNameInBrackets(strings.TrimPrefix(constraint.Key, attrDestLabel))
			if err != nil {
				rbacLog.Errorf("ignored invalid %s: %v", attrDestLabel, err)
				continue
			}
			actualValue, present = service.labels[consLabel]
		} else {
			actualValue, present = service.attributes[constraint.Key]
		}
		// The constraint is not matched if any of the follow condition is true:
		// a) the constraint is specified but not found in the serviceMetadata;
		// b) the constraint value is not matched to the actual value;
		if !present || !stringMatch(actualValue, constraint.Values) {
			return false
		}
	}
	return true
}

// Plugin implements Istio RBAC authz
type Plugin struct{}

// NewPlugin returns an instance of the authz plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.Router {
		return nil
	}

	return buildFilter(in, mutable)
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	// Only supports sidecar proxy for now.
	if in.Node.Type != model.SidecarProxy {
		return nil
	}

	return buildFilter(in, mutable)
}

func buildFilter(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.ServiceInstance == nil || in.ServiceInstance.Service == nil {
		rbacLog.Errorf("nil service instance")
		return nil
	}

	svc := in.ServiceInstance.Service.Hostname
	attr := in.ServiceInstance.Service.Attributes
	authzPolicies := in.Push.AuthzPolicies
	rbacEnabled, globalPermissive := isRbacEnabled(string(svc), attr.Namespace, authzPolicies)
	if !rbacEnabled {
		return nil
	}

	service := createServiceMetadata(&attr, in.ServiceInstance)
	if service == nil {
		rbacLog.Errorf("failed to get service")
		return nil
	}
	option := rbacOption{authzPolicies: authzPolicies, globalPermissiveMode: globalPermissive}

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolTCP:
		rbacLog.Debugf("building filter for TCP listener protocol")
		tcpFilter := buildTCPFilter(service, option, util.IsXDSMarshalingToAnyEnabled(in.Node))
		if in.Node.Type == model.Router || in.Node.Type == model.Ingress {
			// For gateways, due to TLS termination, a listener marked as TCP could very well
			// be using a HTTP connection manager. So check the filterChain.listenerProtocol
			// to decide the type of filter to attach
			httpFilter := buildHTTPFilter(service, option, util.IsXDSMarshalingToAnyEnabled(in.Node))
			rbacLog.Infof("built RBAC http filter for router/ingress %s", service)
			for cnum := range mutable.FilterChains {
				if mutable.FilterChains[cnum].ListenerProtocol == plugin.ListenerProtocolHTTP {
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				} else {
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, *tcpFilter)
				}
			}
		} else {
			rbacLog.Infof("built RBAC tcp filter for sidecar %s", service)
			for cnum := range mutable.FilterChains {
				mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, *tcpFilter)
			}
		}
	case plugin.ListenerProtocolHTTP:
		rbacLog.Debugf("building filter for HTTP listener protocol")
		filter := buildHTTPFilter(service, option, util.IsXDSMarshalingToAnyEnabled(in.Node))
		if filter != nil {
			rbacLog.Infof("built RBAC http filter for %s", service)
			for cnum := range mutable.FilterChains {
				mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
			}
		}
	}

	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

// isServiceInList checks if a given service or namespace is found in the RbacConfig target list.
func isServiceInList(svc string, namespace string, li *rbacproto.RbacConfig_Target) bool {
	if li == nil {
		return false
	}

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

func isRbacEnabled(svc string, ns string, authzPolicies *model.AuthorizationPolicies) (bool /*rbac enabed*/, bool /*permissive mode enabled globally*/) {
	if authzPolicies == nil {
		return false, false
	}

	configProto := authzPolicies.RbacConfig
	if configProto == nil {
		rbacLog.Debugf("disabled, no RbacConfig")
		return false, false
	}

	isPermissive := configProto.EnforcementMode == rbacproto.EnforcementMode_PERMISSIVE
	switch configProto.Mode {
	case rbacproto.RbacConfig_ON:
		return true, isPermissive
	case rbacproto.RbacConfig_ON_WITH_INCLUSION:
		return isServiceInList(svc, ns, configProto.Inclusion), isPermissive
	case rbacproto.RbacConfig_ON_WITH_EXCLUSION:
		return !isServiceInList(svc, ns, configProto.Exclusion), isPermissive
	default:
		rbacLog.Debugf("rbac plugin disabled by RbacConfig: %v", *configProto)
		return false, isPermissive
	}
}

func buildTCPFilter(service *serviceMetadata, option rbacOption, isXDSMarshalingToAnyEnabled bool) *listener.Filter {
	option.forTCPFilter = true
	// The result of convertRbacRulesToFilterConfig() is wrapped in a config for http filter, here we
	// need to extract the generated rules and put in a config for network filter.
	var config *http_config.RBAC
	if option.authzPolicies.IsRbacV2 {
		rbacLog.Debugf("used RBAC v2 for TCP filter")
		config = convertRbacRulesToFilterConfigV2(service, option)
	} else {
		rbacLog.Debugf("used RBAC v1 for TCP filter")
		config = convertRbacRulesToFilterConfig(service, option)
	}
	tcpConfig := listener.Filter{
		Name: rbacTCPFilterName,
	}
	rbacConfig := &network_config.RBAC{
		Rules:       config.Rules,
		ShadowRules: config.ShadowRules,
		StatPrefix:  rbacTCPFilterStatPrefix,
	}

	if isXDSMarshalingToAnyEnabled {
		tcpConfig.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)}
	} else {
		tcpConfig.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(rbacConfig)}
	}

	rbacLog.Debugf("generated tcp filter config: %v", tcpConfig)
	return &tcpConfig
}

// buildHTTPFilter builds the RBAC http filter that enforces the access control to the specified
// service which is co-located with the sidecar proxy.
func buildHTTPFilter(service *serviceMetadata, option rbacOption, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	option.forTCPFilter = false
	var config *http_config.RBAC
	if option.authzPolicies.IsRbacV2 {
		rbacLog.Debugf("used RBAC v2 for HTTP filter")
		config = convertRbacRulesToFilterConfigV2(service, option)
	} else {
		rbacLog.Debugf("used RBAC v1 for HTTP filter")
		config = convertRbacRulesToFilterConfig(service, option)
	}
	rbacLog.Debugf("generated http filter config: %v", *config)
	out := &http_conn.HttpFilter{
		Name: rbacHTTPFilterName,
	}

	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(config)}
	}

	return out
}

// convertRbacRulesToFilterConfig converts the current RBAC rules (ServiceRole and ServiceRoleBindings)
// in service mesh to the corresponding proxy config for the specified service. The generated proxy config
// will be consumed by envoy RBAC filter to enforce access control on the specified service.
func convertRbacRulesToFilterConfig(service *serviceMetadata, option rbacOption) *http_config.RBAC {
	rbac := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}
	permissiveRbac := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}

	namespace := service.attributes[attrDestNamespace]
	roleToBindings := option.authzPolicies.RoleToBindingsForNamespace(namespace)
	for _, role := range option.authzPolicies.RolesForNamespace(namespace) {
		rbacLog.Debugf("checking role %v", role.Name)
		permissions := make([]*policyproto.Permission, 0)
		for i, rule := range role.Spec.(*rbacproto.ServiceRole).Rules {
			if service.match(rule) {
				rbacLog.Debugf("rules[%d] matched", i)
				if option.forTCPFilter {
					// TODO(yangminzhu): Move the validate logic to push context and add metrics.
					if err := validateRuleForTCPFilter(rule); err != nil {
						// It's a user misconfiguration if a HTTP rule is specified to a TCP service.
						// For safety consideration, we ignore the whole rule which means no access is opened to
						// the TCP service in this case.
						rbacLog.Debugf("rules[%d] ignored, found HTTP only rule for a TCP service: %v", i, err)
						continue
					}
				}
				// Generate the policy if the service is matched and validated to the services specified in
				// ServiceRole.
				permissions = append(permissions, convertToPermission(rule))
			}
		}
		if len(permissions) == 0 {
			rbacLog.Debugf("role %s skipped for no rule matched", role.Name)
			continue
		}

		bindings := roleToBindings[role.Name]
		if option.forTCPFilter {
			if err := validateBindingsForTCPFilter(bindings); err != nil {
				rbacLog.Debugf("role %s skipped, found HTTP only binding for a TCP service: %v", role.Name, err)
				continue
			}
		}
		enforcedPrincipals, permissivePrincipals := convertToPrincipals(bindings, option.forTCPFilter)
		if len(enforcedPrincipals) == 0 && len(permissivePrincipals) == 0 {
			rbacLog.Debugf("role %s skipped for no principals found", role.Name)
			continue
		}

		if option.globalPermissiveMode {
			// If RBAC Config is set to permissive mode globally, all policies will be in
			// permissive mode regardless its own mode.
			ps := enforcedPrincipals
			ps = append(ps, permissivePrincipals...)
			if len(ps) != 0 {
				permissiveRbac.Policies[role.Name] = &policyproto.Policy{
					Permissions: permissions,
					Principals:  ps,
				}
			}
		} else {
			if len(enforcedPrincipals) != 0 {
				rbac.Policies[role.Name] = &policyproto.Policy{
					Permissions: permissions,
					Principals:  enforcedPrincipals,
				}
			}

			if len(permissivePrincipals) != 0 {
				permissiveRbac.Policies[role.Name] = &policyproto.Policy{
					Permissions: permissions,
					Principals:  permissivePrincipals,
				}
			}
		}
	}

	// If RBAC Config is set to permissive mode globally, RBAC is transparent to users;
	// when mapping to rbac filter config, there is only shadow rules(no normal rules).
	if option.globalPermissiveMode {
		return &http_config.RBAC{
			ShadowRules: permissiveRbac}
	}

	// If RBAC permissive mode is only set on policy level, set ShadowRules only when there is policy in permissive mode.
	// Otherwise, non-empty shadow_rules causes permissive attributes are sent to mixer when permissive mode isn't set.
	if len(permissiveRbac.Policies) > 0 {
		return &http_config.RBAC{Rules: rbac, ShadowRules: permissiveRbac}
	}

	return &http_config.RBAC{Rules: rbac}
}

// convertToPermission converts a single AccessRule to a Permission.
func convertToPermission(rule *rbacproto.AccessRule) *policyproto.Permission {
	pg := rbacfilter.PermissionGenerator{}

	if len(rule.Hosts) > 0 {
		rule := permissionForKeyValues(hostHeader, rule.Hosts)
		pg.Append(rule)
	}

	if len(rule.NotHosts) > 0 {
		rule := permissionForKeyValues(hostHeader, rule.NotHosts)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(rule.Methods) > 0 {
		rule := permissionForKeyValues(methodHeader, rule.Methods)
		pg.Append(rule)
	}

	if len(rule.NotMethods) > 0 {
		rule := permissionForKeyValues(methodHeader, rule.NotMethods)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(rule.Paths) > 0 {
		rule := permissionForKeyValues(pathHeader, rule.Paths)
		pg.Append(rule)
	}

	if len(rule.NotPaths) > 0 {
		rule := permissionForKeyValues(pathHeader, rule.NotPaths)
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(rule.Ports) > 0 {
		rule := permissionForKeyValues(portKey, convertPortsToString(rule.Ports))
		pg.Append(rule)
	}

	if len(rule.NotPorts) > 0 {
		rule := permissionForKeyValues(portKey, convertPortsToString(rule.NotPorts))
		pg.Append(rbacfilter.PermissionNot(rule))
	}

	if len(rule.Constraints) > 0 {
		// Constraint rule is matched with AND semantics, it's invalid if 2 constraints have the same
		// key and this should already be caught in validation stage.
		for _, constraint := range rule.Constraints {
			rule := permissionForKeyValues(constraint.Key, constraint.Values)
			pg.Append(rule)
		}
	}

	if pg.IsEmpty() {
		// None of above rule satisfied means the permission applies to all paths/methods/constraints.
		pg.Append(rbacfilter.PermissionAny(true))
	}

	return pg.AndPermissions()
}

func permissionForKeyValues(key string, values []string) *policyproto.Permission {
	var converter func(string) (*policyproto.Permission, error)
	switch {
	case key == attrDestIP:
		converter = func(v string) (*policyproto.Permission, error) {
			cidr, err := matcher.CidrRange(v)
			if err != nil {
				return nil, err
			}
			return rbacfilter.PermissionDestinationIP(cidr), nil
		}
	case key == attrDestPort || key == portKey:
		converter = func(v string) (*policyproto.Permission, error) {
			portValue, err := convertToPort(v)
			if err != nil {
				return nil, err
			}
			return rbacfilter.PermissionDestinationPort(portValue), nil
		}
	case key == pathHeader || key == methodHeader || key == hostHeader:
		converter = func(v string) (*policyproto.Permission, error) {
			m := matcher.HeaderMatcher(key, v)
			return rbacfilter.PermissionHeader(m), nil
		}
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		converter = func(v string) (*policyproto.Permission, error) {
			m := matcher.HeaderMatcher(header, v)
			return rbacfilter.PermissionHeader(m), nil
		}
	case key == attrConnSNI:
		converter = func(v string) (*policyproto.Permission, error) {
			m := matcher.StringMatcher(v)
			return rbacfilter.PermissionRequestedServerName(m), nil
		}
	case strings.HasPrefix(key, "experimental.envoy.filters.") && isKeyBinary(key):
		// Split key of format experimental.envoy.filters.a.b[c] to [envoy.filters.a.b, c].
		parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)
		converter = func(v string) (*policyproto.Permission, error) {
			// If value is of format [v], create a list matcher.
			// Else, if value is of format v, create a string matcher.
			var m *envoy_matcher.MetadataMatcher
			if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
				m = matcher.MetadataListMatcher(parts[0], parts[1:], strings.Trim(v, "[]"))
			} else {
				m = matcher.MetadataStringMatcher(parts[0], parts[1], matcher.StringMatcher(v))
			}
			return rbacfilter.PermissionMetadata(m), nil
		}
	default:
		if !attributesEnforcedInPlugin(key) {
			// The attribute is neither matched here nor in previous stage, this means it's something we
			// don't understand, most likely a user typo.
			rbacLog.Errorf("ignored unsupported constraint key: %s", key)
		}
		return nil
	}

	pg := rbacfilter.PermissionGenerator{}
	for _, v := range values {
		if rule, err := converter(v); err != nil {
			rbacLog.Errorf("ignored invalid constraint value: %v", err)
		} else {
			pg.Append(rule)
		}
	}
	return pg.OrPermissions()
}

// convertToPrincipals converts subjects to two lists of principals, one from enforced mode ServiceBindings,
// and the other from permissive mode ServiceBindings.
func convertToPrincipals(bindings []*rbacproto.ServiceRoleBinding, forTCPFilter bool) ([]*policyproto.Principal, []*policyproto.Principal) {
	enforcedPrincipals := make([]*policyproto.Principal, 0)
	permissivePrincipals := make([]*policyproto.Principal, 0)

	for _, binding := range bindings {
		if binding.Mode == rbacproto.EnforcementMode_ENFORCED {
			for _, subject := range binding.Subjects {
				enforcedPrincipals = append(enforcedPrincipals, convertToPrincipal(subject, forTCPFilter))
			}
		} else {
			for _, subject := range binding.Subjects {
				permissivePrincipals = append(permissivePrincipals, convertToPrincipal(subject, forTCPFilter))
			}
		}
	}

	return enforcedPrincipals, permissivePrincipals
}

// TODO(pitlv2109): Refactor first class fields.
// convertToPrincipal converts a single subject to principal.
func convertToPrincipal(subject *rbacproto.Subject, forTCPFilter bool) *policyproto.Principal {
	pg := rbacfilter.PrincipalGenerator{}

	// TODO(pitlv2109): Delete this subject.User block of code once we rolled out 1.2?
	if subject.User != "" {
		if subject.User == "*" {
			pg.Append(rbacfilter.PrincipalAny(true))
		} else {
			var id *policyproto.Principal
			if forTCPFilter {
				// Generate the user directly in Authenticated principal as metadata is not supported in
				// TCP filter.
				m := matcher.StringMatcherWithPrefix(subject.User, spiffe.URIPrefix)
				id = rbacfilter.PrincipalAuthenticated(m)
			} else {
				// Generate the user field with attrSrcPrincipal in the metadata.
				id = principalForKeyValue(attrSrcPrincipal, subject.User, forTCPFilter)
			}
			pg.Append(id)
		}
	}

	// TODO(pitlv2109): Same as above.
	if subject.Group != "" {
		if subject.Properties == nil {
			subject.Properties = make(map[string]string)
		}
		// Treat subject.Group as the request.auth.claims[groups] property. If
		// request.auth.claims[groups] has been defined for the subject, subject.Group
		// overrides request.auth.claims[groups].
		if subject.Properties[attrRequestClaimGroups] != "" {
			rbacLog.Errorf("Both subject.group and request.auth.claims[groups] are defined.\n")
		}
		rbacLog.Debugf("Treat subject.Group (%s) as the request.auth.claims[groups]\n", subject.Group)
		subject.Properties[attrRequestClaimGroups] = subject.Group
	}

	if len(subject.Names) > 0 {
		id := principalForKeyValues(attrSrcPrincipal, subject.Names, forTCPFilter)
		pg.Append(id)
	}

	if len(subject.NotNames) > 0 {
		id := principalForKeyValues(attrSrcPrincipal, subject.NotNames, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(subject.Groups) > 0 {
		id := principalForKeyValues(attrRequestClaimGroups, subject.Groups, forTCPFilter)
		pg.Append(id)
	}

	if len(subject.NotGroups) > 0 {
		id := principalForKeyValues(attrRequestClaimGroups, subject.NotGroups, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(subject.Namespaces) > 0 {
		id := principalForKeyValues(attrSrcNamespace, subject.Namespaces, forTCPFilter)
		pg.Append(id)
	}

	if len(subject.NotNamespaces) > 0 {
		id := principalForKeyValues(attrSrcNamespace, subject.NotNamespaces, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(subject.Ips) > 0 {
		id := principalForKeyValues(attrSrcIP, subject.Ips, forTCPFilter)
		pg.Append(id)
	}

	if len(subject.NotIps) > 0 {
		id := principalForKeyValues(attrSrcIP, subject.NotIps, forTCPFilter)
		pg.Append(rbacfilter.PrincipalNot(id))
	}

	if len(subject.Properties) > 0 {
		// Use a separate key list to make sure the map iteration order is stable, so that the generated
		// config is stable.
		var keys []string
		for k := range subject.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := subject.Properties[k]
			if k == attrSrcPrincipal && subject.User != "" {
				rbacLog.Errorf("ignored %s, duplicate with previous user value %s",
					attrSrcPrincipal, subject.User)
				continue
			}
			id := principalForKeyValue(k, v, forTCPFilter)
			pg.Append(id)
		}
	}

	if pg.IsEmpty() {
		// None of above principal satisfied means nobody has the permission.
		id := rbacfilter.PrincipalNot(rbacfilter.PrincipalAny(true))
		pg.Append(id)
	}
	return pg.AndPrincipals()
}

// principalForKeyValues converts an Istio first class field (e.g. namespaces, groups, ips, as opposed
// to properties fields) to Envoy RBAC config and returns said field as a Principal or returns nil
// if the field is empty.
func principalForKeyValues(key string, values []string, forTCPFilter bool) *policyproto.Principal {
	pg := rbacfilter.PrincipalGenerator{}
	for _, value := range values {
		id := principalForKeyValue(key, value, forTCPFilter)
		pg.Append(id)
	}
	return pg.OrPrincipals()
}

func principalForKeyValue(key, value string, forTCPFilter bool) *policyproto.Principal {
	switch {
	case attrSrcIP == key:
		cidr, err := matcher.CidrRange(value)
		if err != nil {
			rbacLog.Errorf("ignored invalid source ip value: %v", err)
			return nil
		}
		return rbacfilter.PrincipalSourceIP(cidr)
	case attrSrcNamespace == key:
		if forTCPFilter {
			regex := fmt.Sprintf(".*/ns/%s/.*", value)
			m := matcher.StringMatcherRegex(regex)
			return rbacfilter.PrincipalAuthenticated(m)
		}
		// Proxy doesn't have attrSrcNamespace directly, but the information is encoded in attrSrcPrincipal
		// with format: cluster.local/ns/{NAMESPACE}/sa/{SERVICE-ACCOUNT}.
		value = strings.Replace(value, "*", ".*", -1)
		m := matcher.StringMatcherRegex(fmt.Sprintf(".*/ns/%s/.*", value))
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, attrSrcPrincipal, m)
		return rbacfilter.PrincipalMetadata(metadata)
	case attrSrcPrincipal == key:
		if value == allUsers {
			return rbacfilter.PrincipalAny(true)
		}
		// We don't allow users to use "*" in names or not_names. However, we will use "*" internally to
		// refer to authenticated users, since existing code using regex to map "*" to all authenticated
		// users.
		if value == allAuthenticatedUsers {
			value = "*"
		}

		if forTCPFilter {
			m := matcher.StringMatcherWithPrefix(value, spiffe.URIPrefix)
			return rbacfilter.PrincipalAuthenticated(m)
		}
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(metadata)
	case found(key, []string{attrRequestPrincipal, attrRequestAudiences, attrRequestPresenter, attrSrcUser}):
		m := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(m)
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		m := matcher.HeaderMatcher(header, value)
		return rbacfilter.PrincipalHeader(m)
	case strings.HasPrefix(key, attrRequestClaims):
		claim, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestClaims))
		if err != nil {
			return nil
		}
		// Generate a metadata list matcher for the given path keys and value.
		// On proxy side, the value should be of list type.
		m := matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName, []string{attrRequestClaims, claim}, value)
		return rbacfilter.PrincipalMetadata(m)
	default:
		rbacLog.Debugf("generated dynamic metadata matcher for custom property: %s", key)
		filterName := rbacHTTPFilterName
		if forTCPFilter {
			filterName = rbacTCPFilterName
		}
		metadata := matcher.MetadataStringMatcher(filterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(metadata)
	}
}
