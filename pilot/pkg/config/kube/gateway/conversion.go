// Copyright Istio Authors
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

package gateway

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	ControllerName = "istio.io/gateway-controller"
)

type KubernetesResources struct {
	GatewayClass  []config.Config
	Gateway       []config.Config
	HTTPRoute     []config.Config
	TCPRoute      []config.Config
	TLSRoute      []config.Config
	BackendPolicy []config.Config
	Namespaces    map[string]*corev1.Namespace

	// Domain for the cluster. Typically cluster.local
	Domain  string
	Context model.GatewayContext
}

// gatewayLabelSelectorAsSelector is like metav1.LabelSelectorAsSelector but for gateway selector, which
// has different logic
func gatewayLabelSelectorAsSelector(ps *metav1.LabelSelector) (klabels.Selector, error) {
	if ps == nil {
		// LabelSelectorAsSelector returns Nothing() here
		return klabels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(ps)
}

// isRouteMatch checks if a route should bind to a gateway.
// This takes into account selection config on both the gateway and xRoute objects
func isRouteMatch(cfg config.Config, gateway config.Meta,
	routes k8s.RouteBindingSelector, namespaces map[string]*corev1.Namespace) bool {
	// This entire function is a series of condition `return false`s. If everything passes, we consider
	// it a match.

	// First check this config is the right type
	if routes.Kind != cfg.GroupVersionKind.Kind {
		return false
	}

	grp := ""
	if routes.Group != nil {
		grp = *routes.Group
	}
	if grp == "" { // Default group in the spec
		grp = gvk.ServiceApisGateway.Group
	}
	if grp != cfg.GroupVersionKind.Group {
		return false
	}

	// Next, check the label selector matches the gateway
	ls, err := gatewayLabelSelectorAsSelector(routes.Selector)
	if err != nil {
		log.Errorf("failed to create route selector: %v", err)
		return false
	}
	if !ls.Matches(klabels.Set(cfg.Labels)) {
		return false
	}

	// Check the gateway's namespace selector
	namespaceSelector := routes.Namespaces

	// Setup default if not provided
	from := k8s.RouteSelectSame
	if namespaceSelector != nil && namespaceSelector.From != nil && *namespaceSelector.From != "" {
		from = *namespaceSelector.From
	}
	switch from {
	case k8s.RouteSelectAll:
		// Always matches, continue
	case k8s.RouteSelectSame:
		if gateway.Namespace != cfg.Namespace {
			return false
		}
	case k8s.RouteSelectSelector:
		ns, err := metav1.LabelSelectorAsSelector(namespaceSelector.Selector)
		if err != nil {
			log.Errorf("failed to create namespace selector: %v", err)
			return false
		}
		namespace := namespaces[cfg.Namespace]
		if namespace == nil {
			log.Errorf("missing namespace %v for route %v, skipping", cfg.Namespace, cfg.Name)
			return false
		}
		if !ns.Matches(toNamespaceSet(namespace.Name, namespace.Labels)) {
			return false
		}
	}

	gatewaySelector := getGatewaySelectorFromSpec(cfg.Spec)
	allow := k8s.GatewayAllowSameNamespace
	if gatewaySelector != nil && gatewaySelector.Allow != nil {
		allow = *gatewaySelector.Allow
	}
	switch allow {
	case k8s.GatewayAllowAll:
	// Always matches, continue
	case k8s.GatewayAllowFromList:
		found := false
		if gatewaySelector == nil {
			return false
		}
		for _, gw := range gatewaySelector.GatewayRefs {
			if gw.Name == gateway.Name && gw.Namespace == gateway.Namespace {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	case k8s.GatewayAllowSameNamespace:
		if gateway.Namespace != cfg.Namespace {
			return false
		}
	}

	return true
}

// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
const NamespaceNameLabel = "kubernetes.io/metadata.name"

func toNamespaceSet(name string, labels map[string]string) klabels.Labels {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return klabels.Set(labels)
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return klabels.Set(ret)
}

func getGatewaySelectorFromSpec(spec config.Spec) *k8s.RouteGateways {
	switch s := spec.(type) {
	case *k8s.HTTPRouteSpec:
		return s.Gateways
	case *k8s.TCPRouteSpec:
		return s.Gateways
	case *k8s.TLSRouteSpec:
		return s.Gateways
	default:
		return nil
	}
}

func (r *KubernetesResources) fetchHTTPRoutes(gateway config.Meta, routes k8s.RouteBindingSelector) []config.Config {
	result := []config.Config{}
	for _, http := range r.HTTPRoute {
		if isRouteMatch(http, gateway, routes, r.Namespaces) {
			result = append(result, http)
		}
	}
	return result
}

func (r *KubernetesResources) fetchTCPRoutes(gateway config.Meta, routes k8s.RouteBindingSelector) []config.Config {
	result := []config.Config{}
	for _, tcp := range r.TCPRoute {
		if isRouteMatch(tcp, gateway, routes, r.Namespaces) {
			result = append(result, tcp)
		}
	}
	return result
}

func (r *KubernetesResources) fetchTLSRoutes(gateway config.Meta, routes k8s.RouteBindingSelector) []config.Config {
	result := []config.Config{}
	for _, tls := range r.TLSRoute {
		if isRouteMatch(tls, gateway, routes, r.Namespaces) {
			result = append(result, tls)
		}
	}
	return result
}

type OutputResources struct {
	Gateway         []config.Config
	VirtualService  []config.Config
	DestinationRule []config.Config
}

func convertResources(r *KubernetesResources) OutputResources {
	result := OutputResources{}
	gw, routeMap := convertGateways(r)
	result.Gateway = gw
	result.VirtualService = convertVirtualService(r, routeMap)
	result.DestinationRule = convertDestinationRule(r)
	return result
}

// Unique key to identify a route
type RouteKey struct {
	Gvk       config.GroupVersionKind
	Name      string
	Namespace string
}

func toRouteKey(c config.Config) RouteKey {
	return RouteKey{
		c.GroupVersionKind,
		c.Name,
		c.Namespace,
	}
}

func convertDestinationRule(r *KubernetesResources) []config.Config {
	result := []config.Config{}
	for _, obj := range r.BackendPolicy {
		res := convertBackendPolicy(r, obj)
		result = append(result, res...)
	}
	return result
}

// TODO(https://github.com/kubernetes-sigs/gateway-api/issues/590) consider more fields in the API
const BackendPolicyAdmitted = "Admitted"

func convertBackendPolicy(r *KubernetesResources, obj config.Config) []config.Config {
	result := []config.Config{}

	bp := obj.Spec.(*k8s.BackendPolicySpec)

	backendConditions := map[string]*condition{
		BackendPolicyAdmitted: {
			reason:  "Admitted",
			message: "Configuration was valid",
		},
		// TODO actually look in push context to see service exists
		string(k8s.ConditionNoSuchBackend): {
			reason:  "BackendValid",
			message: "All backends are valid",
			status:  kstatus.StatusFalse,
		},
	}
	defer reportBackendPolicyCondition(obj, backendConditions)

	var tlsSettings *istio.ClientTLSSettings
	if bp.TLS != nil && bp.TLS.CertificateAuthorityRef != nil {
		cred, err := buildSecretReference(*bp.TLS.CertificateAuthorityRef)
		if err != nil {
			backendConditions[BackendPolicyAdmitted].error = err
			return result
		}
		tlsSettings = &istio.ClientTLSSettings{
			// Currently, only simple is supported
			CredentialName: cred,
			Mode:           istio.ClientTLSSettings_SIMPLE,
		}
	}
	for i, ref := range bp.BackendRefs {
		var serviceName string
		if emptyOrEqual(ref.Group, gvk.Service.CanonicalGroup()) && emptyOrEqual(ref.Kind, gvk.Service.Kind) {
			// TODO validate this actually exists in service registry?
			serviceName = fmt.Sprintf("%s.%s.svc.%s", ref.Name, obj.Namespace, r.Domain)
		} else {
			backendConditions[string(k8s.ConditionNoSuchBackend)].error = &ConfigError{
				Reason:  InvalidDestination,
				Message: fmt.Sprintf("unsupported backendRef: %+v", ref),
			}
			continue
		}
		dr := &istio.DestinationRule{
			Host:          serviceName,
			TrafficPolicy: &istio.TrafficPolicy{},
		}
		if tlsSettings != nil {
			if ref.Port != nil {
				dr.TrafficPolicy.PortLevelSettings = append(dr.TrafficPolicy.PortLevelSettings, &istio.TrafficPolicy_PortTrafficPolicy{
					Port: &istio.PortSelector{Number: uint32(*ref.Port)},
					Tls:  tlsSettings,
				})
			} else {
				dr.TrafficPolicy.Tls = tlsSettings
			}
		}
		drConfig := config.Config{
			Meta: config.Meta{
				CreationTimestamp: obj.CreationTimestamp,
				GroupVersionKind:  gvk.DestinationRule,
				Name:              fmt.Sprintf("%s-%d-%s", obj.Name, i, constants.KubernetesGatewayName),
				Namespace:         obj.Namespace,
				Domain:            r.Domain,
			},
			Spec: dr,
		}
		result = append(result, drConfig)
	}
	return result
}

func convertVirtualService(r *KubernetesResources, routeMap map[RouteKey][]gatewayReference) []config.Config {
	result := []config.Config{}
	for _, obj := range r.TCPRoute {
		gateways, f := routeMap[toRouteKey(obj)]
		if !f {
			// There are no gateways using this route
			continue
		}

		if vsConfig := buildTCPVirtualService(obj, gateways, r.Domain); vsConfig != nil {
			result = append(result, *vsConfig)
		}
	}

	for _, obj := range r.TLSRoute {
		gateways, f := routeMap[toRouteKey(obj)]
		if !f {
			// There are no gateways using this route
			continue
		}

		if vsConfig := buildTLSVirtualService(obj, gateways, r.Domain); vsConfig != nil {
			result = append(result, *vsConfig)
		}
	}

	for _, obj := range r.HTTPRoute {
		gateways, f := routeMap[toRouteKey(obj)]
		if !f {
			// There are no gateways using this route
			continue
		}

		result = append(result, buildHTTPVirtualServices(obj, gateways, r.Domain)...)
	}
	return result
}

func buildHTTPVirtualServices(obj config.Config, gateways []gatewayReference, domain string) []config.Config {
	result := []config.Config{}

	route := obj.Spec.(*k8s.HTTPRouteSpec)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.HTTPRouteStatus)
			rs.Gateways = createRouteStatus(gateways, obj, rs.Gateways, routeErr)
			return rs
		})
	}

	name := fmt.Sprintf("%s-%s", obj.Name, constants.KubernetesGatewayName)

	httproutes := []*istio.HTTPRoute{}
	hosts := hostnameToStringList(route.Hostnames)
	for _, r := range route.Rules {
		// TODO: implement redirect, rewrite, timeout, mirror, corspolicy, retries
		vs := &istio.HTTPRoute{}
		for _, match := range r.Matches {
			uri, err := createURIMatch(match)
			if err != nil {
				reportError(err)
				return nil
			}
			headers, err := createHeadersMatch(match)
			if err != nil {
				reportError(err)
				return nil
			}
			qp, err := createQueryParamsMatch(match)
			if err != nil {
				reportError(err)
				return nil
			}
			vs.Match = append(vs.Match, &istio.HTTPMatchRequest{
				Uri:         uri,
				Headers:     headers,
				QueryParams: qp,
			})
		}
		for _, filter := range r.Filters {
			switch filter.Type {
			case k8s.HTTPRouteFilterRequestHeaderModifier:
				vs.Headers = createHeadersFilter(filter.RequestHeaderModifier)
			default:
				reportError(&ConfigError{
					Reason:  InvalidFilter,
					Message: fmt.Sprintf("unsupported filter type %q", filter.Type),
				})
				return nil
			}
		}

		route, err := buildHTTPDestination(r.ForwardTo, obj.Namespace, domain)
		if err != nil {
			reportError(err)
			return nil
		}
		vs.Route = route
		httproutes = append(httproutes, vs)
	}
	reportError(nil)
	vsConfig := config.Config{
		Meta: config.Meta{
			CreationTimestamp: obj.CreationTimestamp,
			GroupVersionKind:  gvk.VirtualService,
			Name:              name,
			Namespace:         obj.Namespace,
			Domain:            domain,
		},
		Spec: &istio.VirtualService{
			Hosts:    hosts,
			Gateways: referencesToInternalNames(gateways),
			Http:     httproutes,
		},
	}
	result = append(result, vsConfig)
	return result
}

func hostnameToStringList(h []k8s.Hostname) []string {
	// In the Istio API, empty hostname is not allowed. In the Kubernetes API hosts means "any"
	if len(h) == 0 {
		return []string{"*"}
	}
	res := make([]string, 0, len(h))
	for _, i := range h {
		res = append(res, string(i))
	}
	return res
}

func buildTCPVirtualService(obj config.Config, gateways []gatewayReference, domain string) *config.Config {
	route := obj.Spec.(*k8s.TCPRouteSpec)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TCPRouteStatus)
			rs.Gateways = createRouteStatus(gateways, obj, rs.Gateways, routeErr)
			return rs
		})
	}

	routes := []*istio.TCPRoute{}
	for _, r := range route.Rules {
		route, err := buildTCPDestination(r.ForwardTo, obj.Namespace, domain)
		if err != nil {
			reportError(err)
			return nil
		}
		ir := &istio.TCPRoute{
			Match: buildTCPMatch(r.Matches),
			Route: route,
		}
		routes = append(routes, ir)
	}

	reportError(nil)
	vsConfig := config.Config{
		Meta: config.Meta{
			CreationTimestamp: obj.CreationTimestamp,
			GroupVersionKind:  gvk.VirtualService,
			Name:              fmt.Sprintf("%s-tcp-%s", obj.Name, constants.KubernetesGatewayName),
			Namespace:         obj.Namespace,
			Domain:            domain,
		},
		Spec: &istio.VirtualService{
			// TODO investigate if we should/must constrain this to avoid conflicts
			Hosts:    []string{"*"},
			Gateways: referencesToInternalNames(gateways),
			Tcp:      routes,
		},
	}
	return &vsConfig
}

func buildTLSVirtualService(obj config.Config, gateways []gatewayReference, domain string) *config.Config {
	route := obj.Spec.(*k8s.TLSRouteSpec)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TLSRouteStatus)
			rs.Gateways = createRouteStatus(gateways, obj, rs.Gateways, routeErr)
			return rs
		})
	}

	routes := []*istio.TLSRoute{}
	for _, r := range route.Rules {
		route, err := buildTCPDestination(r.ForwardTo, obj.Namespace, domain)
		if err != nil {
			reportError(err)
			return nil
		}
		ir := &istio.TLSRoute{
			Match: buildTLSMatch(r.Matches),
			Route: route,
		}
		routes = append(routes, ir)
	}

	reportError(nil)
	vsConfig := config.Config{
		Meta: config.Meta{
			CreationTimestamp: obj.CreationTimestamp,
			GroupVersionKind:  gvk.VirtualService,
			Name:              fmt.Sprintf("%s-tls-%s", obj.Name, constants.KubernetesGatewayName),
			Namespace:         obj.Namespace,
			Domain:            domain,
		},
		Spec: &istio.VirtualService{
			// TODO investigate if we should/must constrain this to avoid conflicts
			Hosts:    []string{"*"},
			Gateways: referencesToInternalNames(gateways),
			Tls:      routes,
		},
	}
	return &vsConfig
}

func buildTCPDestination(action []k8s.RouteForwardTo, ns, domain string) ([]*istio.RouteDestination, *ConfigError) {
	if len(action) == 0 {
		return nil, nil
	}

	weights := []int{}
	for _, w := range action {
		wt := 1
		if w.Weight != nil {
			wt = int(*w.Weight)
		}
		weights = append(weights, wt)
	}
	weights = standardizeWeights(weights)
	res := []*istio.RouteDestination{}
	for i, fwd := range action {
		dst, err := buildGenericDestination(fwd, ns, domain)
		if err != nil {
			return nil, err
		}
		res = append(res, &istio.RouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		})
	}
	return res, nil
}

func buildTCPMatch([]k8s.TCPRouteMatch) []*istio.L4MatchAttributes {
	// Currently the spec only supports extensions, which are not currently implemented by Istio.
	return nil
}

func buildTLSMatch(match []k8s.TLSRouteMatch) []*istio.TLSMatchAttributes {
	if len(match) == 0 {
		// Istio validation doesn't like empty match, instead do a match all explicitly
		return []*istio.TLSMatchAttributes{{
			SniHosts: []string{"*"},
		}}
	}
	res := make([]*istio.TLSMatchAttributes, 0, len(match))
	for _, m := range match {
		res = append(res, &istio.TLSMatchAttributes{
			SniHosts: hostnamesToStringlist(m.SNIs),
		})
	}
	return res
}

func hostnamesToStringlist(h []k8s.Hostname) []string {
	res := make([]string, 0, len(h))
	for _, i := range h {
		res = append(res, string(i))
	}
	return res
}

func intSum(n []int) int {
	r := 0
	for _, i := range n {
		r += i
	}
	return r
}

func buildHTTPDestination(action []k8s.HTTPRouteForwardTo, ns string, domain string) ([]*istio.HTTPRouteDestination, *ConfigError) {
	if action == nil {
		return nil, nil
	}

	weights := []int{}
	for _, w := range action {
		wt := 1
		if w.Weight != nil {
			wt = int(*w.Weight)
		}
		weights = append(weights, wt)
	}
	weights = standardizeWeights(weights)
	res := []*istio.HTTPRouteDestination{}
	for i, fwd := range action {
		dst, err := buildDestination(fwd, ns, domain)
		if err != nil {
			return nil, err
		}
		rd := &istio.HTTPRouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		}
		for _, filter := range fwd.Filters {
			switch filter.Type {
			case k8s.HTTPRouteFilterRequestHeaderModifier:
				rd.Headers = createHeadersFilter(filter.RequestHeaderModifier)
			default:
				return nil, &ConfigError{Reason: InvalidFilter, Message: fmt.Sprintf("unsupported filter type %q", filter.Type)}
			}
		}
		res = append(res, rd)
	}
	return res, nil
}

func buildDestination(to k8s.HTTPRouteForwardTo, ns, domain string) (*istio.Destination, *ConfigError) {
	res := &istio.Destination{}
	if to.Port != nil {
		// TODO: "If unspecified, the destination port in the request is used when forwarding to a backendRef or serviceName."
		// We need to link up with the gateway and construct a per gateway virtual service. This is not actually
		// possible with targetPort in some scenarios; need to reconsider the API.
		res.Port = &istio.PortSelector{Number: uint32(*to.Port)}
	}
	if to.ServiceName != nil {
		res.Host = fmt.Sprintf("%s.%s.svc.%s", *to.ServiceName, ns, domain)
	} else if to.BackendRef != nil {
		// TODO support this. Possible supported destinations are VirtualService (delegation), ServiceEntry or some other concept for external service
		// For now we don't support these though, so return an error
		return nil, &ConfigError{Reason: InvalidDestination, Message: "referencing unsupported destination; backendRef is not supported"}
	}
	return res, nil
}

func buildGenericDestination(to k8s.RouteForwardTo, ns, domain string) (*istio.Destination, *ConfigError) {
	res := &istio.Destination{}
	if to.Port != nil {
		// TODO: "If unspecified, the destination port in the request is used when forwarding to a backendRef or serviceName."
		// We need to link up with the gateway and construct a per gateway virtual service. This is not actually
		// possible with targetPort in some scenarios; need to reconsider the API.
		res.Port = &istio.PortSelector{Number: uint32(*to.Port)}
	}
	if to.ServiceName != nil {
		res.Host = fmt.Sprintf("%s.%s.svc.%s", *to.ServiceName, ns, domain)
	} else if to.BackendRef != nil {
		// TODO support this. Possible supported destinations are VirtualService (delegation), ServiceEntry or some other concept for external service
		// For now we don't support these though, so return an error
		return nil, &ConfigError{Reason: InvalidDestination, Message: "referencing unsupported destination; backendRef is not supported"}
	}
	return res, nil
}

// standardizeWeights migrates a list of weights from relative weights, to weights out of 100
// In the event we cannot cleanly move to 100 denominator, we will round up weights in order. See test for details.
// TODO in the future we should probably just make VirtualService support relative weights directly
func standardizeWeights(weights []int) []int {
	if len(weights) == 1 {
		// Instead of setting weight=100 for a single destination, we will not set weight at all
		return []int{0}
	}
	total := intSum(weights)
	if total == 0 {
		// All empty, fallback to even weight
		for i := range weights {
			weights[i] = 1
		}
		total = len(weights)
	}
	results := make([]int, 0, len(weights))
	remainders := make([]float64, 0, len(weights))
	for _, w := range weights {
		perc := float64(w) / float64(total)
		rounded := int(perc * 100)
		remainders = append(remainders, (perc*100)-float64(rounded))
		results = append(results, rounded)
	}
	remaining := 100 - intSum(results)
	order := argsort(remainders)
	for _, idx := range order {
		if remaining <= 0 {
			break
		}
		remaining--
		results[idx]++
	}
	return results
}

type argSlice struct {
	sort.Interface
	idx []int
}

func (s argSlice) Swap(i, j int) {
	s.Interface.Swap(i, j)
	s.idx[i], s.idx[j] = s.idx[j], s.idx[i]
}

func argsort(n []float64) []int {
	s := &argSlice{Interface: sort.Float64Slice(n), idx: make([]int, len(n))}
	for i := range s.idx {
		s.idx[i] = i
	}
	sort.Sort(sort.Reverse(s))
	return s.idx
}

func createHeadersFilter(filter *k8s.HTTPRequestHeaderFilter) *istio.Headers {
	if filter == nil {
		return nil
	}
	return &istio.Headers{
		Request: &istio.Headers_HeaderOperations{
			Add:    filter.Add,
			Remove: filter.Remove,
			Set:    filter.Set,
		},
	}
}

func createQueryParamsMatch(match k8s.HTTPRouteMatch) (map[string]*istio.StringMatch, *ConfigError) {
	if match.QueryParams == nil {
		return nil, nil
	}
	res := map[string]*istio.StringMatch{}
	tp := k8s.QueryParamMatchExact
	if match.QueryParams.Type != nil {
		tp = *match.QueryParams.Type
	}
	switch tp {
	case k8s.QueryParamMatchExact, k8s.QueryParamMatchImplementationSpecific:
		for k, v := range match.QueryParams.Values {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: v},
			}
		}
	case k8s.QueryParamMatchRegularExpression:
		for k, v := range match.QueryParams.Values {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: v},
			}
		}
	default:
		// Should never happen, unless a new field is added
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported QueryParams type", tp)}
	}
	return res, nil
}

func createHeadersMatch(match k8s.HTTPRouteMatch) (map[string]*istio.StringMatch, *ConfigError) {
	if match.Headers == nil {
		return nil, nil
	}
	res := map[string]*istio.StringMatch{}
	tp := k8s.HeaderMatchExact
	if match.Headers.Type != nil {
		tp = *match.Headers.Type
	}
	switch tp {
	case k8s.HeaderMatchExact, k8s.HeaderMatchImplementationSpecific:
		for k, v := range match.Headers.Values {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: v},
			}
		}
	case k8s.HeaderMatchRegularExpression:
		for k, v := range match.Headers.Values {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: v},
			}
		}
	default:
		// Should never happen, unless a new field is added
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported Header type", tp)}
	}
	return res, nil
}

func createURIMatch(match k8s.HTTPRouteMatch) (*istio.StringMatch, *ConfigError) {
	tp := k8s.PathMatchPrefix
	if match.Path.Type != nil {
		tp = *match.Path.Type
	}
	dest := "/"
	if match.Path.Value != nil {
		dest = *match.Path.Value
	}
	switch tp {
	case k8s.PathMatchPrefix, k8s.PathMatchImplementationSpecific:
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Prefix{Prefix: dest},
		}, nil
	case k8s.PathMatchExact:
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: dest},
		}, nil
	case k8s.PathMatchRegularExpression:
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Regex{Regex: dest},
		}, nil
	default:
		// Should never happen, unless a new field is added
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported Path match type", tp)}
	}
}

// getGatewayClass finds all gateway class that are owned by Istio
func getGatewayClasses(r *KubernetesResources) map[string]struct{} {
	classes := map[string]struct{}{}
	for _, obj := range r.GatewayClass {
		gwc := obj.Spec.(*k8s.GatewayClassSpec)
		if gwc.Controller == ControllerName {
			// TODO we can add any settings we need here needed for the controller
			// For now, we have none, so just add a struct
			classes[obj.Name] = struct{}{}

			obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
				gcs := s.(*k8s.GatewayClassStatus)
				gcs.Conditions = kstatus.ConditionallyUpdateCondition(gcs.Conditions, metav1.Condition{
					Type:               string(k8s.GatewayClassConditionStatusAdmitted),
					Status:             kstatus.StatusTrue,
					ObservedGeneration: obj.Generation,
					LastTransitionTime: metav1.Now(),
					Reason:             "Handled",
					Message:            "Handled by Istio controller",
				})
				return gcs
			})
		}
	}
	return classes
}

// gatewayReference refers to a gateway
type gatewayReference struct {
	// Name is the original name of the resource (ie Kubernetes Gateway name)
	Name string
	// InternalName is the internal name of the resource (ie Istio Gateway name)
	InternalName string
	// Namespace is the namespace of the resource
	Namespace string
}

func referencesToInternalNames(refs []gatewayReference) []string {
	ret := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.InternalName == experimentalMeshGatewayName {
			// We namespace the mesh one for some places in the Kubernetes API that requires it, but internally it must
			// be without namespace
			ret = append(ret, r.InternalName)
		} else {
			ret = append(ret, r.Namespace+"/"+r.InternalName)
		}
	}
	return ret
}

func convertGateways(r *KubernetesResources) ([]config.Config, map[RouteKey][]gatewayReference) {
	result := []config.Config{}
	routeToGateway := map[RouteKey][]gatewayReference{}
	classes := getGatewayClasses(r)
	for _, obj := range r.Gateway {
		kgw := obj.Spec.(*k8s.GatewaySpec)
		if _, f := classes[kgw.GatewayClassName]; !f {
			// No gateway class found, this may be meant for another controller; should be skipped.
			continue
		}
		gatewayConditions := map[string]*condition{
			string(k8s.GatewayConditionReady): {
				reason:  "ListenersValid",
				message: "Listeners valid",
			},
			string(k8s.GatewayConditionScheduled): {
				reason:  "ResourcesAvailable",
				message: "Resources available",
			},
		}
		name := obj.Name + "-" + constants.KubernetesGatewayName
		ref := gatewayReference{
			Name:         obj.Name,
			InternalName: name,
			Namespace:    obj.Namespace,
		}
		var servers []*istio.Server
		gatewayServices := []string{}
		skippedAddresses := []string{}
		for _, addr := range kgw.Addresses {
			if addr.Type != nil && *addr.Type != k8s.NamedAddressType {
				skippedAddresses = append(skippedAddresses, addr.Value)
				continue
			}
			// TODO: For now we are using Addresses. There has been some discussion of allowing inline
			// parameters on the class field like a URL, in which case we will probably just use that. See
			// https://github.com/kubernetes-sigs/gateway-api/pull/614
			fqdn := addr.Value
			if !strings.Contains(fqdn, ".") {
				// Short name, expand it
				fqdn = fmt.Sprintf("%s.%s.svc.%s", fqdn, obj.Namespace, r.Domain)
			}
			gatewayServices = append(gatewayServices, fqdn)
		}
		if len(kgw.Addresses) == 0 {
			// If nothing is defined, setup a default
			// TODO: set default in GatewayClass instead.
			// Maybe we only have a default when obj.Namespace == SystemNamespace
			gatewayServices = []string{fmt.Sprintf("istio-ingressgateway.%s.svc.%s", obj.Namespace, r.Domain)}
		}
		for i, l := range kgw.Listeners {
			server, ok := buildListener(obj, l, i)
			if !ok {
				gatewayConditions[string(k8s.GatewayConditionReady)].error = &ConfigError{
					Reason:  string(k8s.GatewayReasonListenersNotValid),
					Message: "One or more listeners was not valid",
				}
				continue
			}
			servers = append(servers, server)

			// TODO support VirtualService direct reference
			for _, http := range r.fetchHTTPRoutes(obj.Meta, l.Routes) {
				k := toRouteKey(http)
				routeToGateway[k] = append(routeToGateway[k], ref)
			}
			for _, tcp := range r.fetchTCPRoutes(obj.Meta, l.Routes) {
				k := toRouteKey(tcp)
				routeToGateway[k] = append(routeToGateway[k], ref)
			}
			for _, tls := range r.fetchTLSRoutes(obj.Meta, l.Routes) {
				k := toRouteKey(tls)
				routeToGateway[k] = append(routeToGateway[k], ref)
			}
		}

		internal, external, warnings := r.Context.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)
		if len(skippedAddresses) > 0 {
			warnings = append(warnings, fmt.Sprintf("Only NamedAddress is supported, ignoring %v", skippedAddresses))
		}
		if len(warnings) > 0 {
			var msg string
			if len(internal) > 0 {
				msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
					humanReadableJoin(internal), strings.Join(warnings, "; "))
			} else {
				msg = fmt.Sprintf("failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
			}
			gatewayConditions[string(k8s.GatewayConditionReady)].error = &ConfigError{
				Reason:  string(k8s.GatewayReasonAddressNotAssigned),
				Message: msg,
			}
		} else {
			gatewayConditions[string(k8s.GatewayConditionReady)].message = fmt.Sprintf("Gateway valid, assigned to service(s) %s", humanReadableJoin(internal))
		}
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			gs := s.(*k8s.GatewayStatus)
			gs.Addresses = make([]k8s.GatewayAddress, 0, len(external))
			for _, addr := range external {
				ip := k8s.IPAddressType
				gs.Addresses = append(gs.Addresses, k8s.GatewayAddress{
					Type:  &ip,
					Value: addr,
				})
			}
			return gs
		})
		reportGatewayCondition(obj, gatewayConditions)

		if len(servers) == 0 {
			continue
		}
		gatewayConfig := config.Config{
			Meta: config.Meta{
				CreationTimestamp: obj.CreationTimestamp,
				GroupVersionKind:  gvk.Gateway,
				Name:              name,
				Namespace:         obj.Namespace,
				Domain:            r.Domain,
				Annotations: map[string]string{
					model.InternalGatewayServiceAnnotation: strings.Join(gatewayServices, ","),
				},
			},
			Spec: &istio.Gateway{
				Servers: servers,
			},
		}
		result = append(result, gatewayConfig)
	}
	for _, k := range r.fetchMeshRoutes() {
		routeToGateway[k] = append(routeToGateway[k], gatewayReference{
			Name: experimentalMeshGatewayName,
			// This is not really namespaced, but some things currently require a namespace
			Namespace:    "default",
			InternalName: experimentalMeshGatewayName,
		})
	}
	return result, routeToGateway
}

func buildListener(obj config.Config, l k8s.Listener, listenerIndex int) (*istio.Server, bool) {
	listenerConditions := map[string]*condition{
		string(k8s.ListenerConditionReady): {
			reason:  "ListenerReady",
			message: "No errors found",
		},
		string(k8s.ListenerConditionDetached): {
			reason:  "ListenerReady",
			message: "No errors found",
			status:  kstatus.StatusFalse,
		},
		string(k8s.ListenerConditionConflicted): {
			reason:  "ListenerReady",
			message: "No errors found",
			status:  kstatus.StatusFalse,
		},
		string(k8s.ListenerConditionResolvedRefs): {
			reason:  "ListenerReady",
			message: "No errors found",
		},
	}
	defer reportListenerCondition(listenerIndex, l, obj, listenerConditions)
	tls, err := buildTLS(l.TLS)
	if err != nil {
		listenerConditions[string(k8s.ListenerConditionReady)].error = &ConfigError{
			Reason:  string(k8s.ListenerReasonInvalid),
			Message: err.Message,
		}
		listenerConditions[string(k8s.ListenerConditionResolvedRefs)].error = &ConfigError{
			Reason:  string(k8s.ListenerReasonInvalidCertificateRef),
			Message: err.Message,
		}
		return nil, false
	}
	server := &istio.Server{
		// Allow all hosts here. Specific routing will be determined by the virtual services
		Hosts: buildHostnameMatch(l.Hostname),
		Port: &istio.Port{
			Number: uint32(l.Port),
			// TODO currently we 1:1 support protocols in the API. If this changes we may
			// need more logic here.
			Protocol: string(l.Protocol),
			Name:     fmt.Sprintf("%d-gateway-%s-%s", listenerIndex, obj.Name, obj.Namespace),
		},
		// TODO support RouteOverride
		Tls: tls,
	}

	return server, true
}

// experimentalMeshGatewayName defines the magic mesh gateway name.
// TODO: replace this with a more suitable API. This is just added now to allow early adopters to experiment with the API
const experimentalMeshGatewayName = "mesh"

func (r *KubernetesResources) fetchMeshRoutes() []RouteKey {
	keys := []RouteKey{}
	// We only look at HTTP routes for now
	// TODO(https://github.com/kubernetes-sigs/gateway-api/issues) add TLS. We can do it today, but its a bit annoying
	// TODO: add TCP. Need an annotation or API change to associate a route with a service (hostname).
	for _, hr := range r.HTTPRoute {
		gatewaySelector := getGatewaySelectorFromSpec(hr.Spec)
		if gatewaySelector == nil || len(gatewaySelector.GatewayRefs) == 0 {
			continue
		}
		for _, ref := range gatewaySelector.GatewayRefs {
			if ref.Name == experimentalMeshGatewayName { // we ignore namespace. it is required in the spec though
				keys = append(keys, toRouteKey(hr))
			}
		}
	}
	return keys
}

func buildTLS(tls *k8s.GatewayTLSConfig) (*istio.ServerTLSSettings, *ConfigError) {
	if tls == nil {
		return nil, nil
	}
	// Explicitly not supported: file mounted
	// Not yet implemented: TLS mode, https redirect, max protocol version, SANs, CipherSuites, VerifyCertificate

	// TODO: "The SNI server_name must match a route host name for the Gateway to route the TLS request."
	// Do we need to do something smarter here to support ^ ?
	out := &istio.ServerTLSSettings{
		HttpsRedirect: false,
	}
	mode := k8s.TLSModeTerminate
	if tls.Mode != nil {
		mode = *tls.Mode
	}
	switch mode {
	case k8s.TLSModeTerminate:
		out.Mode = istio.ServerTLSSettings_SIMPLE
		if tls.CertificateRef == nil {
			// This is required in the API, should be rejected in validation
			return nil, &ConfigError{Reason: InvalidConfiguration, Message: "invalid nil tls certificate ref"}
		}
		cred, err := buildSecretReference(*tls.CertificateRef)
		if err != nil {
			return nil, err
		}
		out.CredentialName = cred
	case k8s.TLSModePassthrough:
		out.Mode = istio.ServerTLSSettings_PASSTHROUGH
	}
	return out, nil
}

func buildSecretReference(ref k8s.LocalObjectReference) (string, *ConfigError) {
	if !emptyOrEqual(ref.Group, gvk.Secret.CanonicalGroup()) || !emptyOrEqual(ref.Kind, gvk.Secret.Kind) {
		return "", &ConfigError{Reason: InvalidTLS, Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", ref)}
	}
	return ref.Name, nil
}

func buildHostnameMatch(hostname *k8s.Hostname) []string {
	// gateway-api hostname semantics match ours, so pass directly. The one
	// exception is they allow unset, which is equivalent to * for us
	if hostname == nil || *hostname == "" {
		return []string{"*"}
	}
	return []string{string(*hostname)}
}

func emptyOrEqual(have, expected string) bool {
	return have == "" || have == expected
}

func StrPointer(s string) *string {
	return &s
}

func humanReadableJoin(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + ", and " + ss[len(ss)-1]
	}
}
