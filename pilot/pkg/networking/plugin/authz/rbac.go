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
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	network_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"

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

func newServiceMetadata(attr *model.ServiceAttributes, in *model.ServiceInstance) *serviceMetadata {
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

	service := newServiceMetadata(&attr, in.ServiceInstance)
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
	enforcedConfig := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}
	permissiveConfig := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}

	namespace := service.attributes[attrDestNamespace]
	roleToBindings := option.authzPolicies.RoleToBindingsForNamespace(namespace)
	for _, roleConfig := range option.authzPolicies.RolesForNamespace(namespace) {
		roleName := roleConfig.Name
		rbacLog.Debugf("checking role %v", roleName)

		var enforcedBindings []*rbacproto.ServiceRoleBinding
		var permissiveBindings []*rbacproto.ServiceRoleBinding
		for _, binding := range roleToBindings[roleName] {
			if binding.Mode == rbacproto.EnforcementMode_PERMISSIVE || option.globalPermissiveMode {
				// If RBAC Config is set to permissive mode globally, all policies will be in
				// permissive mode regardless its own mode.
				permissiveBindings = append(permissiveBindings, binding)
			} else {
				enforcedBindings = append(enforcedBindings, binding)
			}
		}
		setPolicy(enforcedConfig, roleConfig, enforcedBindings, service, option)
		setPolicy(permissiveConfig, roleConfig, permissiveBindings, service, option)
	}

	// If RBAC Config is set to permissive mode globally, RBAC is transparent to users;
	// when mapping to rbac filter config, there is only shadow rules (no normal rules).
	if option.globalPermissiveMode {
		return &http_config.RBAC{ShadowRules: permissiveConfig}
	}

	ret := &http_config.RBAC{Rules: enforcedConfig}
	// If RBAC permissive mode is only set on policy level, set ShadowRules only when there is policy in permissive mode.
	// Otherwise, non-empty shadow_rules causes permissive attributes are sent to mixer when permissive mode isn't set.
	if len(permissiveConfig.Policies) > 0 {
		ret.ShadowRules = permissiveConfig
	}
	return ret
}

func setPolicy(config *policyproto.RBAC, roleConfig model.Config, bindings []*rbacproto.ServiceRoleBinding,
	service *serviceMetadata, option rbacOption) {
	if len(bindings) != 0 {
		role := roleConfig.Spec.(*rbacproto.ServiceRole)
		m := NewModel(role, bindings)
		policy := m.Generate(service, option.forTCPFilter)
		if policy != nil {
			rbacLog.Debugf("generated policy for role: %s", roleConfig.Name)
			config.Policies[roleConfig.Name] = policy
		}
	}
}
