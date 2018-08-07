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
// is controlled by RbacConfig (a singleton custom resource with cluster scope). User could disable
// this plugin by either deleting the RbacConfig or set the RbacConfig.mode to OFF.
// Note: no RbacConfig is created in the deployment of Istio which means this plugin doesn't generate
// any RBAC config by default.
package authz

import (
	"fmt"
	"sort"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	rbacconfig "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
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
	// rbacFilterName is the name of the RBAC filter in envoy.
	rbacFilterName = "envoy.filters.http.rbac"

	// attributes that could be used in both ServiceRoleBinding and ServiceRole.
	attrRequestHeader = "request.headers" // header name is surrounded by brackets, e.g. "request.headers[User-Agent]".

	// attributes that could be used in a ServiceRoleBinding property.
	attrSrcIP            = "source.ip"              // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrSrcNamespace     = "source.namespace"       // e.g. "default".
	attrSrcUser          = "source.user"            // source identity, e.g. "cluster.local/ns/default/sa/productpage".
	attrSrcPrincipal     = "source.principal"       // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal = "request.auth.principal" // authenticated principal of the request.
	attrRequestAudiences = "request.auth.audiences" // intended audience(s) for this authentication information.
	attrRequestPresenter = "request.auth.presenter" // authorized presenter of the credential.
	attrRequestClaims    = "request.auth.claims"    // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".

	// attributes that could be used in a ServiceRole constraint.
	attrDestIP        = "destination.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort      = "destination.port"      // must be in the range [0, 65535].
	attrDestLabel     = "destination.labels"    // label name is surrounded by brackets, e.g. "destination.labels[version]".
	attrDestName      = "destination.name"      // short service name, e.g. "productpage".
	attrDestNamespace = "destination.namespace" // e.g. "default".
	attrDestUser      = "destination.user"      // service account, e.g. "bookinfo-productpage".

	methodHeader = ":method"
	pathHeader   = ":path"
)

// serviceMetadata is a collection of different kind of information about a service.
type serviceMetadata struct {
	name       string            // full qualified service name, e.g. "productpage.default.svc.cluster.local
	labels     map[string]string // labels of the service instance
	attributes map[string]string // additional attributes of the service
}

func createServiceMetadata(attr *model.ServiceAttributes, in *model.ServiceInstance) *serviceMetadata {
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

// attributesEnforcedInDynamicMetadataMatcher returns true if the given attribute should be enforced
// via dynamic metadata matcher. This means these attributes are depending on the output of authn filter.
func attributesEnforcedInDynamicMetadataMatcher(k string) bool {
	switch k {
	case attrSrcNamespace, attrSrcUser, attrSrcPrincipal, attrRequestPrincipal, attrRequestAudiences,
		attrRequestPresenter:
		return true
	}
	return strings.HasPrefix(k, attrRequestClaims)
}

func generateMetadataStringMatcher(keys []string, v *metadata.StringMatcher) *metadata.MetadataMatcher {
	paths := make([]*metadata.MetadataMatcher_PathSegment, 0)
	for _, k := range keys {
		paths = append(paths, &metadata.MetadataMatcher_PathSegment{
			Segment: &metadata.MetadataMatcher_PathSegment_Key{Key: k},
		})
	}
	return &metadata.MetadataMatcher{
		Filter: authn.AuthnFilterName,
		Path:   paths,
		Value: &metadata.MetadataMatcher_Value{
			MatchPattern: &metadata.MetadataMatcher_Value_StringMatch{
				StringMatch: v,
			},
		},
	}
}

// createDynamicMetadataMatcher creates a MetadataMatcher for the given key, value pair.
func createDynamicMetadataMatcher(k, v string) *metadata.MetadataMatcher {
	var keys []string
	forceRegexPattern := false

	if k == attrSrcNamespace {
		// Proxy doesn't have attrSrcNamespace directly, but the information is encoded in attrSrcPrincipal
		// with format: cluster.local/ns/{NAMESPACE}/sa/{SERVICE-ACCOUNT}, so we change the key to
		// attrSrcPrincipal and change the value to a regular expression to match the namespace part.
		keys = []string{attrSrcPrincipal}
		v = fmt.Sprintf(`*/ns/%s/*`, v)
		forceRegexPattern = true
	} else if strings.HasPrefix(k, attrRequestClaims) {
		claim, err := extractNameInBrackets(strings.TrimPrefix(k, attrRequestClaims))
		if err != nil {
			return nil
		}
		keys = []string{attrRequestClaims, claim}
	} else {
		keys = []string{k}
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
				Prefix: v[:len(v)-1],
			},
		}
	} else {
		stringMatcher = &metadata.StringMatcher{
			MatchPattern: &metadata.StringMatcher_Exact{
				Exact: v,
			},
		}
	}

	return generateMetadataStringMatcher(keys, stringMatcher)
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
	// Only supports sidecar proxy of HTTP listener for now.
	if in.Node.Type != model.Sidecar || in.ListenerProtocol != plugin.ListenerProtocolHTTP {
		return nil
	}
	svc := in.ServiceInstance.Service.Hostname
	attr, err := in.Env.GetServiceAttributes(svc)
	if attr == nil || err != nil {
		rbacLog.Errorf("rbac plugin disabled: invalid service %s: %v", svc, err)
		return nil
	}

	if !isRbacEnabled(string(svc), attr.Namespace, in.Env.IstioConfigStore) {
		return nil
	}

	service := createServiceMetadata(attr, in.ServiceInstance)
	rbacLog.Debugf("building filter config for %v", *service)
	filter := buildHTTPFilter(service, in.Env.IstioConfigStore)
	if filter != nil {
		rbacLog.Infof("built filter config for %s", service.name)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
		}
	}

	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(env *model.Environment, node *model.Proxy, push *model.PushStatus, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(env *model.Environment, node *model.Proxy, push *model.PushStatus, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
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

func isRbacEnabled(svc string, ns string, store model.IstioConfigStore) bool {
	var configProto *rbacproto.RbacConfig
	config := store.RbacConfig()
	if config != nil {
		configProto = config.Spec.(*rbacproto.RbacConfig)
	}
	if configProto == nil {
		rbacLog.Debugf("disabled, no RbacConfig")
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
		rbacLog.Debugf("rbac plugin disabled by RbacConfig: %v", *configProto)
		return false
	}
}

// buildHTTPFilter builds the RBAC http filter that enforces the access control to the specified
// service which is co-located with the sidecar proxy.
func buildHTTPFilter(service *serviceMetadata, store model.IstioConfigStore) *http_conn.HttpFilter {
	namespace, present := service.attributes[attrDestNamespace]
	if !present {
		rbacLog.Errorf("no namespace for service %v", service)
		return nil
	}

	roles := store.ServiceRoles(namespace)
	if roles == nil {
		rbacLog.Infof("no service role in namespace %s", namespace)
	}
	bindings := store.ServiceRoleBindings(namespace)
	if bindings == nil {
		rbacLog.Infof("no service role binding in namespace %s", namespace)
	}

	config := convertRbacRulesToFilterConfig(service, roles, bindings)
	rbacLog.Debugf("generated filter config: %v", *config)
	return &http_conn.HttpFilter{
		Name:   rbacFilterName,
		Config: util.MessageToStruct(config),
	}
}

// convertRbacRulesToFilterConfig converts the current RBAC rules (ServiceRole and ServiceRoleBindings)
// in service mesh to the corresponding proxy config for the specified service. The generated proxy config
// will be consumed by envoy RBAC filter to enforce access control on the specified service.
func convertRbacRulesToFilterConfig(
	service *serviceMetadata, roles []model.Config, bindings []model.Config) *rbacconfig.RBAC {
	// roleToBinding maps ServiceRole name to a list of ServiceRoleBindings.
	roleToBinding := map[string][]*rbacproto.ServiceRoleBinding{}
	for _, binding := range bindings {
		bindingProto := binding.Spec.(*rbacproto.ServiceRoleBinding)
		roleName := bindingProto.RoleRef.Name
		if roleName == "" {
			rbacLog.Errorf("ignored invalid binding %s in %s with empty RoleRef.Name",
				binding.Name, binding.Namespace)
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
			rbacLog.Debugf("role skipped for no principals found")
			continue
		}

		rbacLog.Debugf("checking role %v", role.Name)
		permissions := make([]*policyproto.Permission, 0)
		for i, rule := range role.Spec.(*rbacproto.ServiceRole).Rules {
			if service.match(rule) {
				// Generate the policy if the service is matched to the services specified in ServiceRole.
				rbacLog.Debugf("matched AccessRule[%d]", i)
				permissions = append(permissions, convertToPermission(rule))
			}
		}
		if len(permissions) == 0 {
			rbacLog.Debugf("role skipped for no AccessRule matched")
			continue
		}

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
		if subject.User == "*" {
			// Generate an any rule to grant access permission to anyone if the value is "*".
			ids.AndIds.Ids = append(ids.AndIds.Ids, &policyproto.Principal{
				Identifier: &policyproto.Principal_Any{
					Any: true,
				},
			})
		} else {
			// Generate the user field with attrSrcPrincipal in the metadata.
			id := principalForKeyValue(attrSrcPrincipal, subject.User)
			if id != nil {
				ids.AndIds.Ids = append(ids.AndIds.Ids, id)
			}
		}
	}

	if subject.Group != "" {
		rbacLog.Errorf("ignored Subject.group %s, not implemented", subject.Group)
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

func principalForKeyValue(key, value string) *policyproto.Principal {
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
	case attributesEnforcedInDynamicMetadataMatcher(key):
		if matcher := createDynamicMetadataMatcher(key, value); matcher != nil {
			return &policyproto.Principal{
				Identifier: &policyproto.Principal_Metadata{
					Metadata: matcher,
				},
			}
		}
		rbacLog.Errorf("ignored invalid dynamic metadata: %s", key)
		return nil
	default:
		rbacLog.Errorf("ignored unsupported property key: %s", key)
		return nil
	}
}
