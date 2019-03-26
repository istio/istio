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
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
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
	attrSrcIP              = "source.ip"                   // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrSrcNamespace       = "source.namespace"            // e.g. "default".
	attrSrcUser            = "source.user"                 // source identity, e.g. "cluster.local/ns/default/sa/productpage".
	attrSrcPrincipal       = "source.principal"            // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal   = "request.auth.principal"      // authenticated principal of the request.
	attrRequestAudiences   = "request.auth.audiences"      // intended audience(s) for this authentication information.
	attrRequestPresenter   = "request.auth.presenter"      // authorized presenter of the credential.
	attrRequestClaims      = "request.auth.claims"         // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".
	attrRequestClaimGroups = "request.auth.claims[groups]" // groups claim.

	// attributes that could be used in a ServiceRole constraint.
	attrDestIP        = "destination.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort      = "destination.port"      // must be in the range [0, 65535].
	attrDestLabel     = "destination.labels"    // label name is surrounded by brackets, e.g. "destination.labels[version]".
	attrDestName      = "destination.name"      // short service name, e.g. "productpage".
	attrDestNamespace = "destination.namespace" // e.g. "default".
	attrDestUser      = "destination.user"      // service account, e.g. "bookinfo-productpage".
	attrConnSNI       = "connection.sni"        // server name indication, e.g. "www.example.com".

	methodHeader = ":method"
	pathHeader   = ":path"

	spiffePrefix = spiffe.Scheme + "://"
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

// attributesFromAuthN returns true if the given attribute is generated from AuthN filter. This implies
// the attribute should be enforced by dynamic metadata.
func attributesFromAuthN(k string) bool {
	switch k {
	case attrSrcNamespace, attrSrcUser, attrSrcPrincipal, attrRequestPrincipal, attrRequestAudiences,
		attrRequestPresenter:
		return true
	}
	return strings.HasPrefix(k, attrRequestClaims)
}

func generateMetadataStringMatcher(key string, v *metadata.StringMatcher, filterName string) *metadata.MetadataMatcher {
	return &metadata.MetadataMatcher{
		Filter: filterName,
		Path: []*metadata.MetadataMatcher_PathSegment{
			{Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: key}},
		},
		Value: &metadata.ValueMatcher{
			MatchPattern: &metadata.ValueMatcher_StringMatch{
				StringMatch: v,
			},
		},
	}
}

// generateMetadataListMatcher generates a metadata list matcher for the given path keys and value.
func generateMetadataListMatcher(filter string, keys []string, v string) *metadata.MetadataMatcher {
	listMatcher := &metadata.ListMatcher{
		MatchPattern: &metadata.ListMatcher_OneOf{
			OneOf: &metadata.ValueMatcher{
				MatchPattern: &metadata.ValueMatcher_StringMatch{
					StringMatch: createStringMatcher(v, false /* forceRegexPattern */, false /* prependSpiffe */),
				},
			},
		},
	}

	paths := make([]*metadata.MetadataMatcher_PathSegment, 0)
	for _, k := range keys {
		paths = append(paths, &metadata.MetadataMatcher_PathSegment{
			Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: k},
		})
	}

	return &metadata.MetadataMatcher{
		Filter: filter,
		Path:   paths,
		Value: &metadata.ValueMatcher{
			MatchPattern: &metadata.ValueMatcher_ListMatch{
				ListMatch: listMatcher,
			},
		},
	}
}

func createStringMatcher(v string, forceRegexPattern, prependSpiffe bool) *metadata.StringMatcher {
	extraPrefix := ""
	if prependSpiffe {
		extraPrefix = spiffePrefix
	}
	var stringMatcher *metadata.StringMatcher
	// Check if v is "*" first to make sure we won't generate an empty prefix/suffix StringMatcher,
	// the Envoy StringMatcher doesn't allow empty prefix/suffix.
	if v == "*" || forceRegexPattern {
		stringMatcher = &metadata.StringMatcher{
			MatchPattern: &metadata.StringMatcher_Regex{
				Regex: strings.Replace(v, "*", ".*", -1),
			},
		}
	} else if strings.HasPrefix(v, "*") {
		stringMatcher = &metadata.StringMatcher{
			MatchPattern: &metadata.StringMatcher_Suffix{
				Suffix: v[1:],
			},
		}
	} else if strings.HasSuffix(v, "*") {
		stringMatcher = &metadata.StringMatcher{
			MatchPattern: &metadata.StringMatcher_Prefix{
				Prefix: extraPrefix + v[:len(v)-1],
			},
		}
	} else {
		stringMatcher = &metadata.StringMatcher{
			MatchPattern: &metadata.StringMatcher_Exact{
				Exact: extraPrefix + v,
			},
		}
	}
	return stringMatcher
}

// createDynamicMetadataMatcher creates a MetadataMatcher for the given key, value pair.
func createDynamicMetadataMatcher(k, v string, forTCPFilter bool) *metadata.MetadataMatcher {
	filterName := authn.AuthnFilterName
	if k == attrSrcNamespace {
		// Proxy doesn't have attrSrcNamespace directly, but the information is encoded in attrSrcPrincipal
		// with format: cluster.local/ns/{NAMESPACE}/sa/{SERVICE-ACCOUNT}.
		v = fmt.Sprintf(`*/ns/%s/*`, v)
		stringMatcher := createStringMatcher(v, true /* forceRegexPattern */, false /* prependSpiffe */)
		return generateMetadataStringMatcher(attrSrcPrincipal, stringMatcher, filterName)
	} else if strings.HasPrefix(k, attrRequestClaims) {
		claim, err := extractNameInBrackets(strings.TrimPrefix(k, attrRequestClaims))
		if err != nil {
			return nil
		}
		// Generate a metadata list matcher for the given path keys and value.
		// On proxy side, the value should be of list type.
		return generateMetadataListMatcher(authn.AuthnFilterName, []string{attrRequestClaims, claim}, v)
	}

	stringMatcher := createStringMatcher(v, false /* forceRegexPattern */, false /* prependSpiffe */)
	if !attributesFromAuthN(k) {
		rbacLog.Debugf("generated dynamic metadata matcher for custom property: %s", k)
		if forTCPFilter {
			filterName = rbacTCPFilterName
		} else {
			filterName = rbacHTTPFilterName
		}
	}
	return generateMetadataStringMatcher(k, stringMatcher, filterName)
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
	return nil
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

	svc := in.ServiceInstance.Service.Hostname
	attr := in.ServiceInstance.Service.Attributes
	authzPolicies := in.Env.PushContext.AuthzPolicies
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
		rbacLog.Debugf("building tcp filter config for %v", *service)
		filter := buildTCPFilter(service, option, util.IsXDSMarshalingToAnyEnabled(in.Node))
		if filter != nil {
			rbacLog.Infof("built tcp filter config for %s", service.name)
			for cnum := range mutable.FilterChains {
				mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, *filter)
			}
		}
	case plugin.ListenerProtocolHTTP:
		rbacLog.Debugf("building http filter config for %v", *service)
		filter := buildHTTPFilter(service, option, util.IsXDSMarshalingToAnyEnabled(in.Node))
		if filter != nil {
			rbacLog.Infof("built http filter config for %s", service.name)
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
	config := convertRbacRulesToFilterConfig(service, option)
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
	config := convertRbacRulesToFilterConfig(service, option)
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

	if len(rules.AndRules.Rules) == 0 {
		// None of above rule satisfied means the permission applies to all paths/methods/constraints.
		rules.AndRules.Rules = append(rules.AndRules.Rules,
			&policyproto.Permission{Rule: &policyproto.Permission_Any{Any: true}})
	}

	return &policyproto.Permission{Rule: rules}
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

// convertToPrincipal converts a single subject to principal.
func convertToPrincipal(subject *rbacproto.Subject, forTCPFilter bool) *policyproto.Principal {
	ids := &policyproto.Principal_AndIds{
		AndIds: &policyproto.Principal_Set{
			Ids: make([]*policyproto.Principal, 0),
		},
	}

	if subject.User != "" {
		if subject.User == "*" {
			// Generate an any rule to grant access permission to anyone if the value is "*".
			ids.AndIds.Ids = append(ids.AndIds.Ids, &policyproto.Principal{
				Identifier: &policyproto.Principal_Any{
					Any: true,
				},
			})
		} else {
			var id *policyproto.Principal
			if forTCPFilter {
				// Generate the user directly in Authenticated principal as metadata is not supported in
				// TCP filter.
				m := createStringMatcher(subject.User, false /* forceRegexPattern */, forTCPFilter)
				id = principalForStringMatcher(m)
			} else {
				// Generate the user field with attrSrcPrincipal in the metadata.
				id = principalForKeyValue(attrSrcPrincipal, subject.User, forTCPFilter)
			}
			if id != nil {
				ids.AndIds.Ids = append(ids.AndIds.Ids, id)
			}
		}
	}

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

	if len(subject.Properties) != 0 {
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
			if id != nil {
				ids.AndIds.Ids = append(ids.AndIds.Ids, id)
			}
		}
	}

	if len(ids.AndIds.Ids) == 0 {
		// None of above principal satisfied means nobody has the permission.
		ids.AndIds.Ids = append(ids.AndIds.Ids,
			&policyproto.Principal{Identifier: &policyproto.Principal_NotId{
				NotId: &policyproto.Principal{
					Identifier: &policyproto.Principal_Any{Any: true},
				},
			}})
	}

	return &policyproto.Principal{Identifier: ids}
}

func permissionForKeyValues(key string, values []string) *policyproto.Permission {
	var converter func(string) (*policyproto.Permission, error)
	switch {
	case key == attrDestIP:
		converter = func(v string) (*policyproto.Permission, error) {
			cidr, err := convertToCidr(v)
			if err != nil {
				return nil, err
			}
			return &policyproto.Permission{
				Rule: &policyproto.Permission_DestinationIp{DestinationIp: cidr},
			}, nil
		}
	case key == attrDestPort:
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
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		converter = func(v string) (*policyproto.Permission, error) {
			return &policyproto.Permission{
				Rule: &policyproto.Permission_Header{
					Header: convertToHeaderMatcher(header, v),
				},
			}, nil
		}
	case key == attrConnSNI:
		converter = func(v string) (*policyproto.Permission, error) {
			return &policyproto.Permission{
				Rule: &policyproto.Permission_RequestedServerName{
					RequestedServerName: createStringMatcher(v, false /* forceRegexPattern */, false /* prependSpiffe */),
				},
			}, nil
		}
	case strings.HasPrefix(key, "experimental.envoy.filters.") && isKeyBinary(key):
		// Split key of format experimental.envoy.filters.a.b[c] to [envoy.filters.a.b, c].
		parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)
		converter = func(v string) (*policyproto.Permission, error) {
			// If value is of format [v], create a list matcher.
			// Else, if value is of format v, create a string matcher.
			var metadataMatcher *metadata.MetadataMatcher
			if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
				metadataMatcher = generateMetadataListMatcher(parts[0], parts[1:], strings.Trim(v, "[]"))
			} else {
				stringMatcher := createStringMatcher(v, false /* forceRegexPattern */, false /* prependSpiffe */)
				metadataMatcher = generateMetadataStringMatcher(parts[1], stringMatcher, parts[0])
			}

			return &policyproto.Permission{
				Rule: &policyproto.Permission_Metadata{
					Metadata: metadataMatcher,
				},
			}, nil
		}
	default:
		if !attributesEnforcedInPlugin(key) {
			// The attribute is neither matched here nor in previous stage, this means it's something we
			// don't understand, most likely a user typo.
			rbacLog.Errorf("ignored unsupported constraint key: %s", key)
		}
		return nil
	}

	orRules := &policyproto.Permission_OrRules{
		OrRules: &policyproto.Permission_Set{
			Rules: make([]*policyproto.Permission, 0),
		},
	}
	for _, v := range values {
		if p, err := converter(v); err != nil {
			rbacLog.Errorf("ignored invalid constraint value: %v", err)
		} else {
			orRules.OrRules.Rules = append(orRules.OrRules.Rules, p)
		}
	}

	return &policyproto.Permission{Rule: orRules}
}

// Create a Principal based on the key and the value.
// key: the key of a subject property.
// value: the value of a subject property.
// forTCPFilter: the principal is used in the TCP filter.
func principalForKeyValue(key, value string, forTCPFilter bool) *policyproto.Principal {
	if forTCPFilter {
		switch key {
		case attrSrcPrincipal:
			m := createStringMatcher(value, false /* forceRegexPattern */, forTCPFilter)
			return principalForStringMatcher(m)
		case attrSrcNamespace:
			m := createStringMatcher(fmt.Sprintf("*/ns/%s/*", value), true /* forceRegexPattern */, forTCPFilter)
			return principalForStringMatcher(m)
		}
	}

	switch {
	case key == attrSrcIP:
		cidr, err := convertToCidr(value)
		if err != nil {
			rbacLog.Errorf("ignored invalid source ip value: %v", err)
			return nil
		}
		return &policyproto.Principal{Identifier: &policyproto.Principal_SourceIp{SourceIp: cidr}}
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		return &policyproto.Principal{
			Identifier: &policyproto.Principal_Header{
				Header: convertToHeaderMatcher(header, value),
			},
		}
	default:
		if matcher := createDynamicMetadataMatcher(key, value, forTCPFilter); matcher != nil {
			return &policyproto.Principal{
				Identifier: &policyproto.Principal_Metadata{
					Metadata: matcher,
				},
			}
		}
		rbacLog.Errorf("failed to generated dynamic metadata matcher for key: %s", key)
		return nil
	}
}

// principalForStringMatcher generates a principal based on the string matcher.
func principalForStringMatcher(m *metadata.StringMatcher) *policyproto.Principal {
	if m == nil {
		return nil
	}
	return &policyproto.Principal{
		Identifier: &policyproto.Principal_Authenticated_{
			Authenticated: &policyproto.Principal_Authenticated{
				PrincipalName: m,
			},
		},
	}
}
