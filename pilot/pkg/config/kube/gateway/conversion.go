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
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	annotation "istio.io/api/annotation"
	"istio.io/api/label"
	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

const (
	DefaultClassName             = "istio"
	ControllerName               = "istio.io/gateway-controller"
	gatewayAliasForAnnotationKey = "gateway.istio.io/alias-for"
)

// KubernetesResources stores all inputs to our conversion
type KubernetesResources struct {
	GatewayClass    []config.Config
	Gateway         []config.Config
	HTTPRoute       []config.Config
	TCPRoute        []config.Config
	TLSRoute        []config.Config
	ReferencePolicy []config.Config
	// Namespaces stores all namespace in the cluster, keyed by name
	Namespaces map[string]*corev1.Namespace

	// Domain for the cluster. Typically, cluster.local
	Domain  string
	Context model.GatewayContext
}

// OutputResources stores all outputs of our conversion
type OutputResources struct {
	Gateway        []config.Config
	VirtualService []config.Config
	// AllowedReferences stores all allowed references, from Reference -> to Reference(s)
	AllowedReferences map[Reference]map[Reference]*AllowedReferences
	// ReferencedNamespaceKeys stores the label key of all namespace selections. This allows us to quickly
	// determine if a namespace update could have impacted any Gateways. See namespaceEvent.
	ReferencedNamespaceKeys sets.Set
}

// Reference stores a reference to a namespaced GVK, as used by ReferencePolicy
type Reference struct {
	Kind      config.GroupVersionKind
	Namespace k8s.Namespace
}

// convertResources is the top level entrypoint to our conversion logic, computing the full state based
// on KubernetesResources inputs.
func convertResources(r *KubernetesResources) OutputResources {
	// sort HTTPRoutes by creation timestamp and namespace/name
	sort.Slice(r.HTTPRoute, func(i, j int) bool {
		if r.HTTPRoute[i].CreationTimestamp.Equal(r.HTTPRoute[j].CreationTimestamp) {
			in := r.HTTPRoute[i].Namespace + "/" + r.HTTPRoute[i].Name
			jn := r.HTTPRoute[j].Namespace + "/" + r.HTTPRoute[j].Name
			return in < jn
		}
		return r.HTTPRoute[i].CreationTimestamp.Before(r.HTTPRoute[j].CreationTimestamp)
	})
	result := OutputResources{}
	gw, gwMap, nsReferences := convertGateways(r)
	result.Gateway = gw
	result.VirtualService = convertVirtualService(r, gwMap)

	// Once we have gone through all route computation, we will know how many routes bound to each gateway.
	// Report this in the status.
	for _, dm := range gwMap {
		for _, pri := range dm {
			if pri.ReportAttachedRoutes != nil {
				pri.ReportAttachedRoutes()
			}
		}
	}
	result.AllowedReferences = convertReferencePolicies(r)
	result.ReferencedNamespaceKeys = nsReferences
	return result
}

type AllowedReferences struct {
	AllowAll     bool
	AllowedNames sets.Set
}

// convertReferencePolicies extracts all ReferencePolicy into an easily accessibly index.
// The currently supported references are:
// * Gateway -> Secret
func convertReferencePolicies(r *KubernetesResources) map[Reference]map[Reference]*AllowedReferences {
	res := map[Reference]map[Reference]*AllowedReferences{}
	for _, obj := range r.ReferencePolicy {
		rp := obj.Spec.(*k8s.ReferencePolicySpec)
		for _, from := range rp.From {
			fromKey := Reference{
				Namespace: from.Namespace,
			}
			if string(from.Group) == gvk.KubernetesGateway.Group && string(from.Kind) == gvk.KubernetesGateway.Kind {
				fromKey.Kind = gvk.KubernetesGateway
			} else {
				// Not supported type. Not an error; may be for another controller
				continue
			}
			for _, to := range rp.To {
				toKey := Reference{
					Namespace: k8s.Namespace(obj.Namespace),
				}
				if to.Group == "" && string(to.Kind) == gvk.Secret.Kind {
					toKey.Kind = gvk.Secret
				} else {
					// Not supported type. Not an error; may be for another controller
					continue
				}
				if _, f := res[fromKey]; !f {
					res[fromKey] = map[Reference]*AllowedReferences{}
				}
				if _, f := res[fromKey][toKey]; !f {
					res[fromKey][toKey] = &AllowedReferences{
						AllowedNames: sets.New(),
					}
				}
				if to.Name != nil {
					res[fromKey][toKey].AllowedNames.Insert(string(*to.Name))
				} else {
					res[fromKey][toKey].AllowAll = true
				}
			}
		}
	}
	return res
}

// convertVirtualService takes all xRoute types and generates corresponding VirtualServices.
func convertVirtualService(r *KubernetesResources, gatewayMap map[parentKey]map[k8s.SectionName]*parentInfo) []config.Config {
	result := []config.Config{}
	for _, obj := range r.TCPRoute {
		if vsConfig := buildTCPVirtualService(obj, gatewayMap, r.Domain); vsConfig != nil {
			result = append(result, *vsConfig)
		}
	}

	for _, obj := range r.TLSRoute {
		result = append(result, buildTLSVirtualService(obj, gatewayMap, r.Domain)...)
	}

	// for gateway routes, build one VS per gateway+host
	gatewayRoutes := make(map[string]map[string]*config.Config)
	// for mesh routes, build one VS per namespace+host
	meshRoutes := make(map[string]map[string]*config.Config)
	for _, obj := range r.HTTPRoute {
		buildHTTPVirtualServices(obj, gatewayMap, r.Domain, gatewayRoutes, meshRoutes)
	}
	for _, vsByHost := range gatewayRoutes {
		for _, vsConfig := range vsByHost {
			result = append(result, *vsConfig)
		}
	}
	for _, vsByHost := range meshRoutes {
		for _, vsConfig := range vsByHost {
			result = append(result, *vsConfig)
		}
	}
	return result
}

func buildHTTPVirtualServices(obj config.Config, gateways map[parentKey]map[k8s.SectionName]*parentInfo, domain string,
	gatewayRoutes map[string]map[string]*config.Config, meshRoutes map[string]map[string]*config.Config) {
	route := obj.Spec.(*k8s.HTTPRouteSpec)
	for _, r := range route.Rules {
		if len(r.Matches) > 1 {
			// if a rule has multiple matches, make a deep copy to avoid impacting the obj when splitting the rule
			route = route.DeepCopy()
			break
		}
	}
	ns := obj.Namespace
	parentRefs := extractParentReferenceInfo(gateways, route.ParentRefs, route.Hostnames, gvk.HTTPRoute, ns)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.HTTPRouteStatus)
			rs.Parents = createRouteStatus(parentRefs, obj, rs.Parents, routeErr)
			return rs
		})
	}

	httproutes := []*istio.HTTPRoute{}
	hosts := hostnameToStringList(route.Hostnames)
	for i := 0; i < len(route.Rules); i++ {
		r := route.Rules[i]
		if len(r.Matches) > 1 {
			// split the rule to make sure each rule has up to one match
			for _, match := range r.Matches {
				splitRule := r
				splitRule.Matches = []k8s.HTTPRouteMatch{match}
				route.Rules = append(route.Rules, splitRule)
			}
			continue
		}
		// TODO: implement rewrite, timeout, mirror, corspolicy, retries
		vs := &istio.HTTPRoute{}
		for _, match := range r.Matches {
			uri, err := createURIMatch(match)
			if err != nil {
				reportError(err)
				return
			}
			headers, err := createHeadersMatch(match)
			if err != nil {
				reportError(err)
				return
			}
			qp, err := createQueryParamsMatch(match)
			if err != nil {
				reportError(err)
				return
			}
			method, err := createMethodMatch(match)
			if err != nil {
				reportError(err)
				return
			}
			vs.Match = append(vs.Match, &istio.HTTPMatchRequest{
				Uri:         uri,
				Headers:     headers,
				QueryParams: qp,
				Method:      method,
			})
		}
		for _, filter := range r.Filters {
			switch filter.Type {
			case k8s.HTTPRouteFilterRequestHeaderModifier:
				vs.Headers = createHeadersFilter(filter.RequestHeaderModifier)
			case k8s.HTTPRouteFilterRequestRedirect:
				vs.Redirect = createRedirectFilter(filter.RequestRedirect)
			case k8s.HTTPRouteFilterRequestMirror:
				mirror, err := createMirrorFilter(filter.RequestMirror, ns, domain)
				if err != nil {
					reportError(err)
					return
				}
				vs.Mirror = mirror
			default:
				reportError(&ConfigError{
					Reason:  InvalidFilter,
					Message: fmt.Sprintf("unsupported filter type %q", filter.Type),
				})
				return
			}
		}

		zero := true
		for _, w := range r.BackendRefs {
			if w.Weight == nil || (w.Weight != nil && int(*w.Weight) != 0) {
				zero = false
				break
			}
		}
		if zero && vs.Redirect == nil {
			// The spec requires us to 503 when there are no >0 weight backends
			vs.Fault = &istio.HTTPFaultInjection{Abort: &istio.HTTPFaultInjection_Abort{
				Percentage: &istio.Percent{
					Value: 100,
				},
				ErrorType: &istio.HTTPFaultInjection_Abort_HttpStatus{
					HttpStatus: 503,
				},
			}}
		}

		route, err := buildHTTPDestination(r.BackendRefs, ns, domain, zero)
		if err != nil {
			reportError(err)
			return
		}
		vs.Route = route

		httproutes = append(httproutes, vs)
	}
	reportError(nil)

	gatewayNames := referencesToInternalNames(parentRefs)
	if len(gatewayNames) == 0 {
		return
	}
	count := 0
	for _, gw := range gatewayNames {
		// for gateway routes, build one VS per gateway+host
		routeMap := gatewayRoutes
		routeKey := gw
		if gw == "mesh" {
			// for mesh routes, build one VS per namespace+host
			routeMap = meshRoutes
			routeKey = ns
		}
		if _, f := routeMap[routeKey]; !f {
			routeMap[routeKey] = make(map[string]*config.Config)
		}
		// Create one VS per hostname with a single hostname.
		// This ensures we can treat each hostname independently, as the spec requires
		for _, host := range hosts {
			if cfg := routeMap[routeKey][host]; cfg != nil {
				// merge http routes
				vs := cfg.Spec.(*istio.VirtualService)
				vs.Http = append(vs.Http, httproutes...)
			} else {
				name := fmt.Sprintf("%s-%d-%s", obj.Name, count, constants.KubernetesGatewayName)
				routeMap[routeKey][host] = &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         ns,
						Domain:            domain,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{host},
						Gateways: []string{gw},
						Http:     httproutes,
					},
				}
				count++
			}
		}
	}
	for _, vsByHost := range gatewayRoutes {
		for _, cfg := range vsByHost {
			vs := cfg.Spec.(*istio.VirtualService)
			sortHTTPRoutes(vs.Http)
		}
	}
	for _, vsByHost := range meshRoutes {
		for _, cfg := range vsByHost {
			vs := cfg.Spec.(*istio.VirtualService)
			sortHTTPRoutes(vs.Http)
		}
	}
}

func routeMeta(obj config.Config) map[string]string {
	m := parentMeta(obj, nil)
	m[constants.InternalRouteSemantics] = constants.RouteSemanticsGateway
	return m
}

// sortHTTPRoutes sorts generated vs routes to meet gateway-api requirements
// see https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1alpha2.HTTPRouteRule
func sortHTTPRoutes(routes []*istio.HTTPRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		if len(routes[i].Match) == 0 {
			return len(routes[j].Match) != 0
		}
		if len(routes[j].Match) == 0 {
			return false
		}
		m1, m2 := routes[i].Match[0], routes[j].Match[0]
		len1, len2 := getURILength(m1), getURILength(m2)
		if len1 == len2 {
			if len(m1.Headers) == len(m2.Headers) {
				return len(m1.QueryParams) > len(m2.QueryParams)
			}
			return len(m1.Headers) > len(m2.Headers)
		}
		return len1 > len2
	})
}

func getURILength(match *istio.HTTPMatchRequest) int {
	if match.Uri == nil {
		return 0
	}
	switch match.Uri.MatchType.(type) {
	case *istio.StringMatch_Prefix:
		return len(match.Uri.GetPrefix())
	case *istio.StringMatch_Exact:
		return len(match.Uri.GetExact())
	case *istio.StringMatch_Regex:
		return len(match.Uri.GetRegex())
	}
	// should not happen
	return -1
}

func parentMeta(obj config.Config, sectionName *k8s.SectionName) map[string]string {
	name := fmt.Sprintf("%s/%s.%s", obj.GroupVersionKind.Kind, obj.Name, obj.Namespace)
	if sectionName != nil {
		name = fmt.Sprintf("%s/%s/%s.%s", obj.GroupVersionKind.Kind, obj.Name, *sectionName, obj.Namespace)
	}
	return map[string]string{
		constants.InternalParentName: name,
	}
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

func toInternalParentReference(p k8s.ParentReference, localNamespace string) (parentKey, error) {
	empty := parentKey{}
	grp := defaultIfNil((*string)(p.Group), gvk.KubernetesGateway.Group)
	kind := defaultIfNil((*string)(p.Kind), gvk.KubernetesGateway.Kind)
	var ik config.GroupVersionKind
	var ns string
	// Currently supported types are Gateway and Mesh
	if kind == gvk.KubernetesGateway.Kind && grp == gvk.KubernetesGateway.Group {
		// Unset namespace means "same namespace"
		ns = defaultIfNil((*string)(p.Namespace), localNamespace)
		ik = gvk.KubernetesGateway
	} else if kind == meshGVK.Kind && grp == meshGVK.Group {
		ik = meshGVK
	} else {
		return empty, fmt.Errorf("unsupported parentKey: %v/%v", grp, kind)
	}
	return parentKey{
		Kind:      ik,
		Name:      string(p.Name),
		Namespace: ns,
	}, nil
}

func referenceAllowed(p *parentInfo, routeKind config.GroupVersionKind, parentKind config.GroupVersionKind, hostnames []k8s.Hostname, namespace string) error {
	// First check the hostnames are a match. This is a bi-directional wildcard match. Only one route
	// hostname must match for it to be allowed (but the others will be filtered at runtime)
	// If either is empty its treated as a wildcard which always matches

	if len(hostnames) == 0 {
		hostnames = []k8s.Hostname{"*"}
	}
	if len(p.Hostnames) > 0 {
		// TODO: the spec actually has a label match, not a string match. That is, *.com does not match *.apple.com
		// We are doing a string match here
		matched := false
		hostMatched := false
		for _, routeHostname := range hostnames {
			for _, parentHostNamespace := range p.Hostnames {
				spl := strings.Split(parentHostNamespace, "/")
				parentNamespace, parentHostname := spl[0], spl[1]
				hostnameMatch := host.Name(parentHostname).Matches(host.Name(routeHostname))
				namespaceMatch := parentNamespace == "*" || parentNamespace == namespace
				hostMatched = hostMatched || hostnameMatch
				if hostnameMatch && namespaceMatch {
					matched = true
					break
				}
			}
		}
		if !matched {
			if hostMatched {
				return fmt.Errorf("hostnames matched parent hostname %q, but namespace %q is not allowed by the parent", p.OriginalHostname, namespace)
			}
			return fmt.Errorf("no hostnames matched parent hostname %q", p.OriginalHostname)
		}
	}
	// Also make sure this route kind is allowed
	if len(p.AllowedKinds) > 0 {
		matched := false
		for _, ak := range p.AllowedKinds {
			if string(ak.Kind) == routeKind.Kind && defaultIfNil((*string)(ak.Group), gvk.GatewayClass.Group) == routeKind.Group {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("kind %v is not allowed", routeKind)
		}
	}

	if parentKind == meshGVK {
		for _, h := range hostnames {
			if h == "*" {
				return fmt.Errorf("mesh requires hostname to be set")
			}
		}
	}
	return nil
}

func extractParentReferenceInfo(gateways map[parentKey]map[k8s.SectionName]*parentInfo, routeRefs []k8s.ParentReference,
	hostnames []k8s.Hostname, kind config.GroupVersionKind, localNamespace string) []routeParentReference {
	parentRefs := []routeParentReference{}
	for _, ref := range routeRefs {
		ir, err := toInternalParentReference(ref, localNamespace)
		if err != nil {
			// Cannot handle the reference. Maybe it is for another controller, so we just ignore it
			continue
		}
		appendParent := func(pr *parentInfo, pk parentKey) {
			rpi := routeParentReference{
				InternalName:      pr.InternalName,
				DeniedReason:      referenceAllowed(pr, kind, pk.Kind, hostnames, localNamespace),
				OriginalReference: ref,
			}
			if rpi.DeniedReason == nil {
				// Record that we were able to bind to the parent
				pr.AttachedRoutes++
			}
			parentRefs = append(parentRefs, rpi)
		}
		if ref.SectionName != nil {
			// We are selecting a specific section, so attach just that section
			if pr, f := gateways[ir][*ref.SectionName]; f {
				appendParent(pr, ir)
			}
		} else {
			// no section name set, match all sections
			for _, pr := range gateways[ir] {
				appendParent(pr, ir)
			}
		}
	}
	return parentRefs
}

func buildTCPVirtualService(obj config.Config, gateways map[parentKey]map[k8s.SectionName]*parentInfo, domain string) *config.Config {
	route := obj.Spec.(*k8s.TCPRouteSpec)

	parentRefs := extractParentReferenceInfo(gateways, route.ParentRefs, nil, gvk.TCPRoute, obj.Namespace)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TCPRouteStatus)
			rs.Parents = createRouteStatus(parentRefs, obj, rs.Parents, routeErr)
			return rs
		})
	}
	gatewayNames := referencesToInternalNames(parentRefs)
	if len(gatewayNames) == 0 {
		reportError(nil)
		return nil
	}

	routes := []*istio.TCPRoute{}
	for _, r := range route.Rules {
		route, err := buildTCPDestination(r.BackendRefs, obj.Namespace, domain)
		if err != nil {
			reportError(err)
			return nil
		}
		ir := &istio.TCPRoute{
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
			Annotations:       routeMeta(obj),
			Namespace:         obj.Namespace,
			Domain:            domain,
		},
		Spec: &istio.VirtualService{
			// We can use wildcard here since each listener can have at most one route bound to it, so we have
			// a single VS per Gateway.
			Hosts:    []string{"*"},
			Gateways: gatewayNames,
			Tcp:      routes,
		},
	}
	return &vsConfig
}

func buildTLSVirtualService(obj config.Config, gateways map[parentKey]map[k8s.SectionName]*parentInfo, domain string) []config.Config {
	route := obj.Spec.(*k8s.TLSRouteSpec)

	parentRefs := extractParentReferenceInfo(gateways, route.ParentRefs, nil, gvk.TLSRoute, obj.Namespace)

	reportError := func(routeErr *ConfigError) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TLSRouteStatus)
			rs.Parents = createRouteStatus(parentRefs, obj, rs.Parents, routeErr)
			return rs
		})
	}

	routes := []*istio.TLSRoute{}
	for _, r := range route.Rules {
		dest, err := buildTCPDestination(r.BackendRefs, obj.Namespace, domain)
		if err != nil {
			reportError(err)
			return nil
		}
		if len(dest) == 0 {
			return nil
		}
		ir := &istio.TLSRoute{
			Match: buildTLSMatch(route.Hostnames),
			Route: dest,
		}
		routes = append(routes, ir)
	}

	reportError(nil)
	gatewayNames := referencesToInternalNames(parentRefs)
	if len(gatewayNames) == 0 {
		// TODO we need to properly return not admitted here
		return nil
	}
	configs := make([]config.Config, 0, len(route.Hostnames))
	for i, host := range hostnameToStringList(route.Hostnames) {
		name := fmt.Sprintf("%s-tls-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
		// Create one VS per hostname with a single hostname.
		// This ensures we can treat each hostname independently, as the spec requires
		vsConfig := config.Config{
			Meta: config.Meta{
				CreationTimestamp: obj.CreationTimestamp,
				GroupVersionKind:  gvk.VirtualService,
				Name:              name,
				Annotations:       routeMeta(obj),
				Namespace:         obj.Namespace,
				Domain:            domain,
			},
			Spec: &istio.VirtualService{
				Hosts:    []string{host},
				Gateways: gatewayNames,
				Tls:      routes,
			},
		}
		configs = append(configs, vsConfig)
	}
	return configs
}

func buildTCPDestination(forwardTo []k8s.BackendRef, ns, domain string) ([]*istio.RouteDestination, *ConfigError) {
	if forwardTo == nil {
		return nil, nil
	}

	weights := []int{}
	action := []k8s.BackendRef{}
	for i, w := range forwardTo {
		wt := 1
		if w.Weight != nil {
			wt = int(*w.Weight)
		}
		if wt == 0 {
			continue
		}
		action = append(action, forwardTo[i])
		weights = append(weights, wt)
	}
	weights = standardizeWeights(weights)
	res := []*istio.RouteDestination{}
	for i, fwd := range action {
		dst, err := buildDestination(fwd, ns, domain)
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

func buildTLSMatch(hostnames []k8s.Hostname) []*istio.TLSMatchAttributes {
	// Currently, the spec only supports extensions beyond hostname, which are not currently implemented by Istio.
	return []*istio.TLSMatchAttributes{{
		SniHosts: hostnamesToStringListWithWildcard(hostnames),
	}}
}

func hostnamesToStringListWithWildcard(h []k8s.Hostname) []string {
	if len(h) == 0 {
		return []string{"*"}
	}
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

func buildHTTPDestination(forwardTo []k8s.HTTPBackendRef, ns string, domain string, totalZero bool) ([]*istio.HTTPRouteDestination, *ConfigError) {
	if forwardTo == nil {
		return nil, nil
	}

	weights := []int{}
	action := []k8s.HTTPBackendRef{}
	for i, w := range forwardTo {
		wt := 1
		if w.Weight != nil {
			wt = int(*w.Weight)
		}
		// When total weight is zero, create destination to add falutInjection.
		// When total weight is not zero, do not create the destination.
		if wt == 0 && !totalZero {
			continue
		}
		action = append(action, forwardTo[i])
		weights = append(weights, wt)
	}
	weights = standardizeWeights(weights)
	res := []*istio.HTTPRouteDestination{}
	for i, fwd := range action {
		dst, err := buildDestination(fwd.BackendRef, ns, domain)
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

func buildDestination(to k8s.BackendRef, ns, domain string) (*istio.Destination, *ConfigError) {
	namespace := defaultIfNil((*string)(to.Namespace), ns)
	if nilOrEqual((*string)(to.Group), "") && nilOrEqual((*string)(to.Kind), gvk.Service.Kind) {
		// Service
		if to.Port == nil {
			// "Port is required when the referent is a Kubernetes Service."
			return nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
		}
		if strings.Contains(string(to.Name), ".") {
			return nil, &ConfigError{Reason: InvalidDestination, Message: "serviceName invalid; the name of the Service must be used, not the hostname."}
		}
		return &istio.Destination{
			// TODO: implement ReferencePolicy for cross namespace
			Host: fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, domain),
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, nil
	}
	if nilOrEqual((*string)(to.Group), features.MCSAPIGroup) && nilOrEqual((*string)(to.Kind), "ServiceImport") {
		// Service import
		name := fmt.Sprintf("%s.%s.svc.clusterset.local", to.Name, namespace)
		if !features.EnableMCSHost {
			// They asked for ServiceImport, but actually don't have full support enabled...
			// No problem, we can just treat it as Service, which is already cross-cluster in this mode anyways
			name = fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, domain)
		}
		if to.Port == nil {
			// We don't know where to send without port
			return nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
		}
		if strings.Contains(string(to.Name), ".") {
			return nil, &ConfigError{Reason: InvalidDestination, Message: "serviceName invalid; the name of the Service must be used, not the hostname."}
		}
		return &istio.Destination{
			Host: name,
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, nil
	}
	if nilOrEqual((*string)(to.Group), gvk.ServiceEntry.Group) && nilOrEqual((*string)(to.Kind), "Hostname") {
		// Hostname synthetic type
		if to.Port == nil {
			// We don't know where to send without port
			return nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
		}
		if to.Namespace != nil {
			return nil, &ConfigError{Reason: InvalidDestination, Message: "namespace may not be set with Hostname type"}
		}
		return &istio.Destination{
			Host: string(to.Name),
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, nil
	}
	return nil, &ConfigError{
		Reason:  InvalidDestination,
		Message: fmt.Sprintf("referencing unsupported backendRef: group %q kind %q", emptyIfNil((*string)(to.Group)), emptyIfNil((*string)(to.Kind))),
	}
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

func headerListToMap(hl []k8s.HTTPHeader) map[string]string {
	if len(hl) == 0 {
		return nil
	}
	res := map[string]string{}
	for _, e := range hl {
		k := strings.ToLower(string(e.Name))
		if _, f := res[k]; f {
			// "Subsequent entries with an equivalent header name MUST be ignored"
			continue
		}
		res[k] = e.Value
	}
	return res
}

func createMirrorFilter(filter *k8s.HTTPRequestMirrorFilter, ns, domain string) (*istio.Destination, *ConfigError) {
	if filter == nil {
		return nil, nil
	}
	var weightOne int32 = 1
	return buildDestination(k8s.BackendRef{
		BackendObjectReference: filter.BackendRef,
		Weight:                 &weightOne,
	}, ns, domain)
}

func createRedirectFilter(filter *k8s.HTTPRequestRedirectFilter) *istio.HTTPRedirect {
	if filter == nil {
		return nil
	}
	resp := &istio.HTTPRedirect{}
	if filter.StatusCode != nil {
		// Istio allows 301, 302, 303, 307, 308.
		// Gateway allows only 301 and 302.
		resp.RedirectCode = uint32(*filter.StatusCode)
	}
	if filter.Hostname != nil {
		resp.Authority = string(*filter.Hostname)
	}
	if filter.Scheme != nil {
		// Both allow http and https
		resp.Scheme = *filter.Scheme
	}
	if filter.Port != nil {
		resp.RedirectPort = &istio.HTTPRedirect_Port{Port: uint32(*filter.Port)}
	} else {
		// "When empty, port (if specified) of the request is used."
		// this differs from Istio default
		resp.RedirectPort = &istio.HTTPRedirect_DerivePort{DerivePort: istio.HTTPRedirect_FROM_REQUEST_PORT}
	}
	return resp
}

func createHeadersFilter(filter *k8s.HTTPRequestHeaderFilter) *istio.Headers {
	if filter == nil {
		return nil
	}
	return &istio.Headers{
		Request: &istio.Headers_HeaderOperations{
			Add:    headerListToMap(filter.Add),
			Remove: filter.Remove,
			Set:    headerListToMap(filter.Set),
		},
	}
}

// nolint: unparam
func createMethodMatch(match k8s.HTTPRouteMatch) (*istio.StringMatch, *ConfigError) {
	if match.Method == nil {
		return nil, nil
	}
	return &istio.StringMatch{
		MatchType: &istio.StringMatch_Exact{Exact: string(*match.Method)},
	}, nil
}

func createQueryParamsMatch(match k8s.HTTPRouteMatch) (map[string]*istio.StringMatch, *ConfigError) {
	res := map[string]*istio.StringMatch{}
	for _, qp := range match.QueryParams {
		tp := k8s.QueryParamMatchExact
		if qp.Type != nil {
			tp = *qp.Type
		}
		switch tp {
		case k8s.QueryParamMatchExact:
			res[qp.Name] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: qp.Value},
			}
		case k8s.QueryParamMatchRegularExpression:
			res[qp.Name] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: qp.Value},
			}
		default:
			// Should never happen, unless a new field is added
			return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported QueryParams type", tp)}
		}
	}

	if len(res) == 0 {
		return nil, nil
	}
	return res, nil
}

func createHeadersMatch(match k8s.HTTPRouteMatch) (map[string]*istio.StringMatch, *ConfigError) {
	res := map[string]*istio.StringMatch{}
	for _, header := range match.Headers {
		tp := k8s.HeaderMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case k8s.HeaderMatchExact:
			res[string(header.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: header.Value},
			}
		case k8s.HeaderMatchRegularExpression:
			res[string(header.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: header.Value},
			}
		default:
			// Should never happen, unless a new field is added
			return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported HeaderMatch type", tp)}
		}
	}

	if len(res) == 0 {
		return nil, nil
	}
	return res, nil
}

func createURIMatch(match k8s.HTTPRouteMatch) (*istio.StringMatch, *ConfigError) {
	tp := k8s.PathMatchPathPrefix
	if match.Path.Type != nil {
		tp = *match.Path.Type
	}
	dest := "/"
	if match.Path.Value != nil {
		dest = *match.Path.Value
	}
	switch tp {
	case k8s.PathMatchPathPrefix:
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
	builtinClassExists := false
	for _, obj := range r.GatewayClass {
		gwc := obj.Spec.(*k8s.GatewayClassSpec)
		if obj.Name == DefaultClassName {
			builtinClassExists = true
		}
		if gwc.ControllerName == ControllerName {
			// TODO we can add any settings we need here needed for the controller
			// For now, we have none, so just add a struct
			classes[obj.Name] = struct{}{}

			obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
				gcs := s.(*k8s.GatewayClassStatus)
				gcs.Conditions = kstatus.UpdateConditionIfChanged(gcs.Conditions, metav1.Condition{
					Type:               string(k8s.GatewayClassConditionStatusAccepted),
					Status:             kstatus.StatusTrue,
					ObservedGeneration: obj.Generation,
					LastTransitionTime: metav1.Now(),
					Reason:             string(k8s.GatewayClassConditionStatusAccepted),
					Message:            "Handled by Istio controller",
				})
				return gcs
			})
		}
	}
	if !builtinClassExists {
		// Allow `istio` class without explicit GatewayClass. However, if it already exists then do not
		// add it here, in case it points to a different controller.
		classes[DefaultClassName] = struct{}{}
	}
	return classes
}

var meshGVK = config.GroupVersionKind{
	Group:   gvk.KubernetesGateway.Group,
	Version: gvk.KubernetesGateway.Version,
	Kind:    "Mesh",
}

// parentKey holds info about a parentRef (ie route binding to a Gateway). This is a mirror of
// k8s.ParentReference in a form that can be stored in a map
type parentKey struct {
	Kind config.GroupVersionKind
	// Name is the original name of the resource (ie Kubernetes Gateway name)
	Name string
	// Namespace is the namespace of the resource
	Namespace string
}

// parentInfo holds info about a "parent" - something that can be referenced as a ParentRef in the API.
// Today, this is just Gateway and Mesh.
type parentInfo struct {
	// InternalName refers to the internal name we can reference it by. For example, "mesh" or "my-ns/my-gateway"
	InternalName string
	// AllowedKinds indicates which kinds can be admitted by this parent
	AllowedKinds []k8s.RouteGroupKind
	// Hostnames is the hostnames that must be match to reference to the parent. For gateway this is listener hostname
	// Format is ns/hostname
	Hostnames []string
	// OriginalHostname is the unprocessed form of Hostnames; how it appeared in users' config
	OriginalHostname string

	// AttachedRoutes keeps track of how many routes are attached to this parent. This is tracked for status.
	// Because this is mutate in the route generation, parentInfo must be passed as a pointer
	AttachedRoutes int32
	// ReportAttachedRoutes is a callback that should be triggered once all AttachedRoutes are computed, to
	// actually store the attached route count in the status
	ReportAttachedRoutes func()
}

// routeParentReference holds information about a route's parent reference
type routeParentReference struct {
	// InternalName refers to the internal name of the parent we can reference it by. For example, "mesh" or "my-ns/my-gateway"
	InternalName string
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason error
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
}

// referencesToInternalNames converts valid parent references to names that can be used in VirtualService
func referencesToInternalNames(parents []routeParentReference) []string {
	ret := make([]string, 0, len(parents))
	for _, p := range parents {
		if p.DeniedReason != nil {
			// We should filter this out
			continue
		}
		ret = append(ret, p.InternalName)
	}
	// To ensure deterministic order, sort them
	sort.Strings(ret)
	return ret
}

func convertGateways(r *KubernetesResources) ([]config.Config, map[parentKey]map[k8s.SectionName]*parentInfo, sets.Set) {
	// result stores our generated Istio Gateways
	result := []config.Config{}
	// gwMap stores an index to access parentInfo (which corresponds to a Kubernetes Gateway)
	gwMap := map[parentKey]map[k8s.SectionName]*parentInfo{}
	// namespaceLabelReferences keeps track of all namespace label keys referenced by Gateways. This is
	// used to ensure we handle namespace updates for those keys.
	namespaceLabelReferences := sets.New()
	classes := getGatewayClasses(r)
	for _, obj := range r.Gateway {
		obj := obj
		kgw := obj.Spec.(*k8s.GatewaySpec)
		if _, f := classes[string(kgw.GatewayClassName)]; !f {
			// No gateway class found, this may be meant for another controller; should be skipped.
			continue
		}

		// Setup initial conditions to the success state. If we encounter errors, we will update this.
		gatewayConditions := map[string]*condition{
			string(k8s.GatewayConditionReady): {
				reason:  "ListenersValid",
				message: "Listeners valid",
			},
		}
		if isManaged(kgw) {
			gatewayConditions[string(k8s.GatewayConditionScheduled)] = &condition{
				error: &ConfigError{
					Reason:  "ResourcesPending",
					Message: "Resources not yet deployed to the cluster",
				},
				setOnce: string(k8s.GatewayReasonNotReconciled), // Default reason
			}
		} else {
			gatewayConditions[string(k8s.GatewayConditionScheduled)] = &condition{
				reason:  "ResourcesAvailable",
				message: "Resources available",
			}
		}
		servers := []*istio.Server{}

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, skippedAddresses := extractGatewayServices(r, kgw, obj)
		invalidListeners := []k8s.SectionName{}
		for i, l := range kgw.Listeners {
			i := i
			namespaceLabelReferences.InsertAll(getNamespaceLabelReferences(l.AllowedRoutes)...)
			server, ok := buildListener(r, obj, l, i)
			if !ok {
				invalidListeners = append(invalidListeners, l.Name)
				continue
			}
			meta := parentMeta(obj, &l.Name)
			meta[model.InternalGatewayServiceAnnotation] = strings.Join(gatewayServices, ",")
			// Each listener generates an Istio Gateway with a single Server. This allows binding to a specific listener.
			gatewayConfig := config.Config{
				Meta: config.Meta{
					CreationTimestamp: obj.CreationTimestamp,
					GroupVersionKind:  gvk.Gateway,
					Name:              fmt.Sprintf("%s-%s-%s", obj.Name, constants.KubernetesGatewayName, l.Name),
					Annotations:       meta,
					Namespace:         obj.Namespace,
					Domain:            r.Domain,
				},
				Spec: &istio.Gateway{
					Servers: []*istio.Server{server},
				},
			}
			ref := parentKey{
				Kind:      gvk.KubernetesGateway,
				Name:      obj.Name,
				Namespace: obj.Namespace,
			}
			if _, f := gwMap[ref]; !f {
				gwMap[ref] = map[k8s.SectionName]*parentInfo{}
			}

			pri := &parentInfo{
				InternalName:     obj.Namespace + "/" + gatewayConfig.Name,
				AllowedKinds:     generateSupportedKinds(l),
				Hostnames:        server.Hosts,
				OriginalHostname: emptyIfNil((*string)(l.Hostname)),
			}
			pri.ReportAttachedRoutes = func() {
				reportListenerAttachedRoutes(i, obj, pri.AttachedRoutes)
			}
			gwMap[ref][l.Name] = pri
			result = append(result, gatewayConfig)
			servers = append(servers, server)
		}

		// If "gateway.istio.io/alias-for" annotation is present, any Route
		// that binds to the gateway will bind to its alias instead.
		// The typical usage is when the original gateway is not managed by the gateway controller
		// but the ( generated ) alias is. This allows people to build their own
		// gateway controllers on top of Istio Gateway Controller.
		if obj.Annotations != nil && obj.Annotations[gatewayAliasForAnnotationKey] != "" {
			ref := parentKey{
				Kind:      gvk.KubernetesGateway,
				Name:      obj.Annotations[gatewayAliasForAnnotationKey],
				Namespace: obj.Namespace,
			}
			alias := parentKey{
				Kind:      gvk.KubernetesGateway,
				Name:      obj.Name,
				Namespace: obj.Namespace,
			}
			gwMap[ref] = gwMap[alias]
		}

		internal, external, warnings := r.Context.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)
		if len(skippedAddresses) > 0 {
			warnings = append(warnings, fmt.Sprintf("Only Hostname is supported, ignoring %v", skippedAddresses))
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
		} else if len(invalidListeners) > 0 {
			gatewayConditions[string(k8s.GatewayConditionReady)].error = &ConfigError{
				Reason:  string(k8s.GatewayReasonListenersNotValid),
				Message: fmt.Sprintf("Invalid listeners: %v", invalidListeners),
			}
		} else {
			gatewayConditions[string(k8s.GatewayConditionReady)].message = fmt.Sprintf("Gateway valid, assigned to service(s) %s", humanReadableJoin(internal))
		}
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			gs := s.(*k8s.GatewayStatus)
			addressesToReport := external
			addrType := k8s.IPAddressType
			if len(addressesToReport) == 0 {
				// There are no external addresses, so report the internal ones
				// TODO: should we always report both?
				addressesToReport = internal
				addrType = k8s.HostnameAddressType
			}
			gs.Addresses = make([]k8s.GatewayAddress, 0, len(addressesToReport))
			for _, addr := range addressesToReport {
				gs.Addresses = append(gs.Addresses, k8s.GatewayAddress{
					Type:  &addrType,
					Value: addr,
				})
			}
			return gs
		})
		reportGatewayCondition(obj, gatewayConditions)
	}
	// Insert a parent for Mesh references.
	gwMap[parentKey{
		Kind: meshGVK,
		Name: "istio",
	}] = map[k8s.SectionName]*parentInfo{
		"": {
			InternalName: "mesh",
		},
	}
	return result, gwMap, namespaceLabelReferences
}

// isManaged checks if a Gateway is managed (ie we create the Deployment and Service) or unmanaged.
// This is based on the address field of the spec. If address is set with a Hostname type, it should point to an existing
// Service that handles the gateway traffic. If it is not set, or refers to only a single IP, we will consider it managed and provision the Service.
// If there is an IP, we will set the `loadBalancerIP` type.
// While there is no defined standard for this in the API yet, it is tracked in https://github.com/kubernetes-sigs/gateway-api/issues/892.
// So far, this mirrors how out of clusters work (address set means to use existing IP, unset means to provision one),
// and there has been growing consensus on this model for in cluster deployments.
//
// Currently, the supported options are:
// * 1 Hostname value. This can be short Service name ingress, or FQDN ingress.ns.svc.cluster.local, example.com. If its a non-k8s FQDN it is a ServiceEntry.
// * 1 IP address. This is managed, with IP explicit
// * Nothing. This is managed, with IP auto assigned
//
// Not supported:
// Multiple hostname/IP - It is feasible but preference is to create multiple Gateways. This would also break the 1:1 mapping of GW:Service
// Mixed hostname and IP - doesn't make sense; user should define the IP in service
// NamedAddress - Service has no concept of named address. For cloud's that have named addresses they can be configured by annotations,
//   which users can add to the Gateway.
func isManaged(gw *k8s.GatewaySpec) bool {
	if len(gw.Addresses) == 0 {
		return true
	}
	if len(gw.Addresses) > 1 {
		return false
	}
	if t := gw.Addresses[0].Type; t == nil || *t == k8s.IPAddressType {
		return true
	}
	return false
}

func extractGatewayServices(r *KubernetesResources, kgw *k8s.GatewaySpec, obj config.Config) ([]string, []string) {
	if isManaged(kgw) {
		return []string{fmt.Sprintf("%s.%s.svc.%v", obj.Name, obj.Namespace, r.Domain)}, nil
	}
	gatewayServices := []string{}
	skippedAddresses := []string{}
	for _, addr := range kgw.Addresses {
		if addr.Type != nil && *addr.Type != k8s.HostnameAddressType {
			// We only support HostnameAddressType. Keep track of invalid ones so we can report in status.
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
	return gatewayServices, skippedAddresses
}

// getNamespaceLabelReferences fetches all label keys used in namespace selectors. Return order may not be stable.
func getNamespaceLabelReferences(routes *k8s.AllowedRoutes) []string {
	if routes == nil || routes.Namespaces == nil || routes.Namespaces.Selector == nil {
		return nil
	}
	res := []string{}
	for k := range routes.Namespaces.Selector.MatchLabels {
		res = append(res, k)
	}
	for _, me := range routes.Namespaces.Selector.MatchExpressions {
		res = append(res, me.Key)
	}
	return res
}

func buildListener(r *KubernetesResources, obj config.Config, l k8s.Listener, listenerIndex int) (*istio.Server, bool) {
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
	tls, err := buildTLS(l.TLS, obj.Namespace, isAutoPassthrough(obj, l))
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
	hostnames := buildHostnameMatch(obj.Namespace, r, l)
	server := &istio.Server{
		Port: &istio.Port{
			// Name is required. We only have one server per Gateway, so we can just name them all the same
			Name:     "default",
			Number:   uint32(l.Port),
			Protocol: listenerProtocolToIstio(l.Protocol),
		},
		Hosts: hostnames,
		Tls:   tls,
	}

	return server, true
}

// isAutoPassthrough determines if a listener should use auto passthrough mode. This is used for
// multi-network. In the Istio API, this is an explicit tls.Mode. However, this mode is not part of
// the gateway-api, and leaks implementation details. We already have an API to declare a Gateway as
// a multinetwork gateway, so we will use this as a signal.
// A user who wishes to expose multinetwork connectivity should create a listener with port 15443 (by default, overridable by label),
// and declare it as PASSTRHOUGH
func isAutoPassthrough(obj config.Config, l k8s.Listener) bool {
	_, networkSet := obj.Labels[label.TopologyNetwork.Name]
	if !networkSet {
		return false
	}
	expectedPort := "15443"

	if port, f := obj.Labels[annotation.NetworkingGatewayPort.Name]; f {
		expectedPort = port
	}
	return fmt.Sprint(l.Port) == expectedPort
}

func listenerProtocolToIstio(protocol k8s.ProtocolType) string {
	// Currently, all gateway-api protocols are valid Istio protocols.
	return string(protocol)
}

func buildTLS(tls *k8s.GatewayTLSConfig, namespace string, isAutoPassthrough bool) (*istio.ServerTLSSettings, *ConfigError) {
	if tls == nil {
		return nil, nil
	}
	// Explicitly not supported: file mounted
	// Not yet implemented: TLS mode, https redirect, max protocol version, SANs, CipherSuites, VerifyCertificate

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
		if len(tls.CertificateRefs) != 1 {
			// This is required in the API, should be rejected in validation
			return nil, &ConfigError{Reason: InvalidConfiguration, Message: "exactly 1 certificateRefs should be present for TLS termination"}
		}
		cred, err := buildSecretReference(*tls.CertificateRefs[0], namespace)
		if err != nil {
			return nil, err
		}
		out.CredentialName = cred
	case k8s.TLSModePassthrough:
		out.Mode = istio.ServerTLSSettings_PASSTHROUGH
		if isAutoPassthrough {
			out.Mode = istio.ServerTLSSettings_AUTO_PASSTHROUGH
		}
	}
	return out, nil
}

func buildSecretReference(ref k8s.SecretObjectReference, defaultNamespace string) (string, *ConfigError) {
	if !nilOrEqual((*string)(ref.Group), gvk.Secret.Group) || !nilOrEqual((*string)(ref.Kind), gvk.Secret.Kind) {
		return "", &ConfigError{Reason: InvalidTLS, Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", objectReferenceString(ref))}
	}
	return credentials.ToKubernetesGatewayResource(defaultIfNil((*string)(ref.Namespace), defaultNamespace), string(ref.Name)), nil
}

func objectReferenceString(ref k8s.SecretObjectReference) string {
	return fmt.Sprintf("%s/%s/%s.%s",
		emptyIfNil((*string)(ref.Group)),
		emptyIfNil((*string)(ref.Kind)),
		ref.Name,
		emptyIfNil((*string)(ref.Namespace)))
}

func parentRefString(ref k8s.ParentReference) string {
	return fmt.Sprintf("%s/%s/%s/%s.%s",
		emptyIfNil((*string)(ref.Group)),
		emptyIfNil((*string)(ref.Kind)),
		ref.Name,
		emptyIfNil((*string)(ref.SectionName)),
		emptyIfNil((*string)(ref.Namespace)))
}

// buildHostnameMatch generates a VirtualService.spec.hosts section from a listener
func buildHostnameMatch(localNamespace string, r *KubernetesResources, l k8s.Listener) []string {
	// We may allow all hostnames or a specific one
	hostname := "*"
	if l.Hostname != nil {
		hostname = string(*l.Hostname)
	}

	resp := []string{}
	for _, ns := range namespacesFromSelector(localNamespace, r, l.AllowedRoutes) {
		resp = append(resp, fmt.Sprintf("%s/%s", ns, hostname))
	}

	// If nothing matched use ~ namespace (match nothing). We need this since its illegal to have an
	// empty hostname list, but we still need the Gateway provisioned to ensure status is properly set and
	// SNI matches are established; we just don't want to actually match any routing rules (yet).
	if len(resp) == 0 {
		return []string{"~/" + hostname}
	}
	return resp
}

// namespacesFromSelector determines a list of allowed namespaces for a given AllowedRoutes
func namespacesFromSelector(localNamespace string, r *KubernetesResources, lr *k8s.AllowedRoutes) []string {
	// Default is to allow only the same namespace
	if lr == nil || lr.Namespaces == nil || lr.Namespaces.From == nil || *lr.Namespaces.From == k8s.NamespacesFromSame {
		return []string{localNamespace}
	}
	if *lr.Namespaces.From == k8s.NamespacesFromAll {
		return []string{"*"}
	}

	if lr.Namespaces.Selector == nil {
		// Should never happen, invalid config
		return []string{"*"}
	}

	// gateway-api has selectors, but Istio Gateway just has a list of names. We will run the selector
	// against all namespaces and get a list of matching namespaces that can be converted into a list
	// Istio can handle.
	ls, err := metav1.LabelSelectorAsSelector(lr.Namespaces.Selector)
	if err != nil {
		return nil
	}
	namespaces := []string{}
	for _, ns := range r.Namespaces {
		if ls.Matches(toNamespaceSet(ns.Name, ns.Labels)) {
			namespaces = append(namespaces, ns.Name)
		}
	}
	// Ensure stable order
	sort.Strings(namespaces)
	return namespaces
}

func emptyIfNil(s *string) string {
	return defaultIfNil(s, "")
}

func defaultIfNil(s *string, d string) string {
	if s != nil {
		return *s
	}
	return d
}

func nilOrEqual(have *string, expected string) bool {
	return have == nil || *have == expected
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

// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
const NamespaceNameLabel = "kubernetes.io/metadata.name"

// toNamespaceSet converts a set of namespace labels to a Set that can be used to select against.
func toNamespaceSet(name string, labels map[string]string) klabels.Set {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return labels
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return ret
}
