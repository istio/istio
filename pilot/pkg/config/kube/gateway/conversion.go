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
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// convertResources is the top level entrypoint to our conversion logic, computing the full state based
// on KubernetesResources inputs.
func convertResources(r GatewayResources) IstioResources {
	// sort HTTPRoutes by creation timestamp and namespace/name
	sort.Slice(r.HTTPRoute, func(i, j int) bool {
		if r.HTTPRoute[i].CreationTimestamp.Equal(r.HTTPRoute[j].CreationTimestamp) {
			in := r.HTTPRoute[i].Namespace + "/" + r.HTTPRoute[i].Name
			jn := r.HTTPRoute[j].Namespace + "/" + r.HTTPRoute[j].Name
			return in < jn
		}
		return r.HTTPRoute[i].CreationTimestamp.Before(r.HTTPRoute[j].CreationTimestamp)
	})
	result := IstioResources{}
	ctx := configContext{
		GatewayResources:   r,
		AllowedReferences:  convertReferencePolicies(r),
		resourceReferences: make(map[model.ConfigKey][]model.ConfigKey),
	}

	gw, gwMap, nsReferences := convertGateways(ctx)
	ctx.GatewayReferences = gwMap
	result.Gateway = gw

	result.VirtualService = convertVirtualService(ctx)

	// Once we have gone through all route computation, we will know how many routes bound to each gateway.
	// Report this in the status.
	for _, dm := range gwMap {
		for _, pri := range dm {
			if pri.ReportAttachedRoutes != nil {
				pri.ReportAttachedRoutes()
			}
		}
	}
	result.AllowedReferences = ctx.AllowedReferences
	result.ReferencedNamespaceKeys = nsReferences
	result.ResourceReferences = ctx.resourceReferences
	return result
}

// convertReferencePolicies extracts all ReferencePolicy into an easily accessibly index.
func convertReferencePolicies(r GatewayResources) AllowedReferences {
	res := map[Reference]map[Reference]*Grants{}
	type namespacedGrant struct {
		Namespace string
		Grant     *k8s.ReferenceGrantSpec
	}
	specs := make([]namespacedGrant, 0, len(r.ReferenceGrant))

	for _, obj := range r.ReferenceGrant {
		rp := obj.Spec.(*k8s.ReferenceGrantSpec)
		specs = append(specs, namespacedGrant{Namespace: obj.Namespace, Grant: rp})
	}
	for _, ng := range specs {
		rp := ng.Grant
		for _, from := range rp.From {
			fromKey := Reference{
				Namespace: from.Namespace,
			}
			if string(from.Group) == gvk.KubernetesGateway.Group && string(from.Kind) == gvk.KubernetesGateway.Kind {
				fromKey.Kind = gvk.KubernetesGateway
			} else if string(from.Group) == gvk.HTTPRoute.Group && string(from.Kind) == gvk.HTTPRoute.Kind {
				fromKey.Kind = gvk.HTTPRoute
			} else if string(from.Group) == gvk.TLSRoute.Group && string(from.Kind) == gvk.TLSRoute.Kind {
				fromKey.Kind = gvk.TLSRoute
			} else if string(from.Group) == gvk.TCPRoute.Group && string(from.Kind) == gvk.TCPRoute.Kind {
				fromKey.Kind = gvk.TCPRoute
			} else {
				// Not supported type. Not an error; may be for another controller
				continue
			}
			for _, to := range rp.To {
				toKey := Reference{
					Namespace: k8s.Namespace(ng.Namespace),
				}
				if to.Group == "" && string(to.Kind) == gvk.Secret.Kind {
					toKey.Kind = gvk.Secret
				} else if to.Group == "" && string(to.Kind) == gvk.Service.Kind {
					toKey.Kind = gvk.Service
				} else {
					// Not supported type. Not an error; may be for another controller
					continue
				}
				if _, f := res[fromKey]; !f {
					res[fromKey] = map[Reference]*Grants{}
				}
				if _, f := res[fromKey][toKey]; !f {
					res[fromKey][toKey] = &Grants{
						AllowedNames: sets.New[string](),
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
func convertVirtualService(r configContext) []config.Config {
	result := []config.Config{}
	for _, obj := range r.TCPRoute {
		result = append(result, buildTCPVirtualService(r, obj)...)
	}

	for _, obj := range r.TLSRoute {
		result = append(result, buildTLSVirtualService(r, obj)...)
	}

	// for gateway routes, build one VS per gateway+host
	gatewayRoutes := make(map[string]map[string]*config.Config)
	// for mesh routes, build one VS per namespace+host
	meshRoutes := make(map[string]map[string]*config.Config)
	for _, obj := range r.HTTPRoute {
		buildHTTPVirtualServices(r, obj, gatewayRoutes, meshRoutes)
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

func convertHTTPRoute(r k8s.HTTPRouteRule, ctx configContext,
	obj config.Config, pos int, enforceRefGrant bool,
) (*istio.HTTPRoute, *ConfigError) {
	// TODO: implement rewrite, timeout, mirror, corspolicy, retries
	vs := &istio.HTTPRoute{}
	// Auto-name the route. If upstream defines an explicit name, will use it instead
	// The position within the route is unique
	vs.Name = fmt.Sprintf("%s.%s.%d", obj.Namespace, obj.Name, pos)

	for _, match := range r.Matches {
		uri, err := createURIMatch(match)
		if err != nil {
			return nil, err
		}
		headers, err := createHeadersMatch(match)
		if err != nil {
			return nil, err
		}
		qp, err := createQueryParamsMatch(match)
		if err != nil {
			return nil, err
		}
		method, err := createMethodMatch(match)
		if err != nil {
			return nil, err
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
		case k8sbeta.HTTPRouteFilterRequestHeaderModifier:
			h := createHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Request = h
		case k8sbeta.HTTPRouteFilterResponseHeaderModifier:
			h := createHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Response = h
		case k8sbeta.HTTPRouteFilterRequestRedirect:
			vs.Redirect = createRedirectFilter(filter.RequestRedirect)
		case k8sbeta.HTTPRouteFilterRequestMirror:
			mirror, err := createMirrorFilter(ctx, filter.RequestMirror, obj.Namespace, enforceRefGrant)
			if err != nil {
				return nil, err
			}
			vs.Mirror = mirror
		case k8sbeta.HTTPRouteFilterURLRewrite:
			vs.Rewrite = createRewriteFilter(filter.URLRewrite)
		default:
			return nil, &ConfigError{
				Reason:  InvalidFilter,
				Message: fmt.Sprintf("unsupported filter type %q", filter.Type),
			}
		}
	}

	if weightSum(r.BackendRefs) == 0 && vs.Redirect == nil {
		// The spec requires us to return 500 when there are no >0 weight backends
		vs.DirectResponse = &istio.HTTPDirectResponse{
			Status: 500,
		}
	} else {
		route, backendErr, err := buildHTTPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant)
		if err != nil {
			return nil, err
		}
		vs.Route = route
		return vs, backendErr
	}

	return vs, nil
}

func parentTypes(rpi []routeParentReference) (mesh, gateway bool) {
	for _, r := range rpi {
		if r.IsMesh() {
			mesh = true
		} else {
			gateway = true
		}
	}
	return
}

func buildHTTPVirtualServices(
	ctx configContext,
	obj config.Config,
	gatewayRoutes map[string]map[string]*config.Config,
	meshRoutes map[string]map[string]*config.Config,
) {
	route := obj.Spec.(*k8s.HTTPRouteSpec)
	parentRefs := extractParentReferenceInfo(ctx.GatewayReferences, route.ParentRefs, route.Hostnames, gvk.HTTPRoute, obj.Namespace)
	reportStatus := func(results []RouteParentResult) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.HTTPRouteStatus)
			rs.Parents = createRouteStatus(results, obj, rs.Parents)
			return rs
		})
	}

	type conversionResult struct {
		error  *ConfigError
		routes []*istio.HTTPRoute
	}
	convertRules := func(mesh bool) conversionResult {
		res := conversionResult{}
		for n, r := range route.Rules {
			// split the rule to make sure each rule has up to one match
			matches := slices.Reference(r.Matches)
			if len(matches) == 0 {
				matches = append(matches, nil)
			}
			for _, m := range matches {
				if m != nil {
					r.Matches = []k8s.HTTPRouteMatch{*m}
				}
				vs, err := convertHTTPRoute(r, ctx, obj, n, !mesh)
				// This was a hard error
				if vs == nil {
					res.error = err
					return conversionResult{error: err}
				}
				// Got an error but also routes
				if err != nil {
					res.error = err
				}

				res.routes = append(res.routes, vs)
			}
		}
		return res
	}
	meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)

	reportStatus(slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
		res := RouteParentResult{
			OriginalReference: r.OriginalReference,
			DeniedReason:      r.DeniedReason,
			RouteError:        gwResult.error,
		}
		if r.IsMesh() {
			res.RouteError = meshResult.error
		}
		return res
	}))
	count := 0
	for _, parent := range filteredReferences(parentRefs) {
		// for gateway routes, build one VS per gateway+host
		routeMap := gatewayRoutes
		routeKey := parent.InternalName
		vsHosts := hostnameToStringList(route.Hostnames)
		routes := gwResult.routes
		if parent.IsMesh() {
			routes = meshResult.routes
			// for mesh routes, build one VS per namespace/port->host
			routeMap = meshRoutes
			routeKey = obj.Namespace
			if parent.OriginalReference.Port != nil {
				routes = augmentPortMatch(routes, *parent.OriginalReference.Port)
				routeKey += fmt.Sprintf("/%d", *parent.OriginalReference.Port)
			}
			vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s",
				parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, k8s.Namespace(obj.Namespace)), ctx.Domain)}
		}
		if len(routes) == 0 {
			continue
		}
		if _, f := routeMap[routeKey]; !f {
			routeMap[routeKey] = make(map[string]*config.Config)
		}

		// Create one VS per hostname with a single hostname.
		// This ensures we can treat each hostname independently, as the spec requires
		for _, h := range vsHosts {
			if cfg := routeMap[routeKey][h]; cfg != nil {
				// merge http routes
				vs := cfg.Spec.(*istio.VirtualService)
				vs.Http = append(vs.Http, routes...)
				// append parents
				cfg.Annotations[constants.InternalParentNames] = fmt.Sprintf("%s,%s/%s.%s",
					cfg.Annotations[constants.InternalParentNames], obj.GroupVersionKind.Kind, obj.Name, obj.Namespace)
			} else {
				name := fmt.Sprintf("%s-%d-%s", obj.Name, count, constants.KubernetesGatewayName)
				routeMap[routeKey][h] = &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.Domain,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{h},
						Gateways: []string{parent.InternalName},
						Http:     routes,
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

func buildMeshAndGatewayRoutes[T any](parentRefs []routeParentReference, convertRules func(mesh bool) T) (T, T) {
	var meshResult, gwResult T
	needMesh, needGw := parentTypes(parentRefs)
	if needMesh {
		meshResult = convertRules(true)
	}
	if needGw {
		gwResult = convertRules(false)
	}
	return meshResult, gwResult
}

func augmentPortMatch(routes []*istio.HTTPRoute, port k8sbeta.PortNumber) []*istio.HTTPRoute {
	res := make([]*istio.HTTPRoute, 0, len(routes))
	for _, r := range routes {
		r = r.DeepCopy()
		for _, m := range r.Match {
			m.Port = uint32(port)
		}
		if len(r.Match) == 0 {
			r.Match = []*istio.HTTPMatchRequest{{
				Port: uint32(port),
			}}
		}
		res = append(res, r)
	}
	return res
}

func augmentTCPPortMatch(routes []*istio.TCPRoute, port k8sbeta.PortNumber) []*istio.TCPRoute {
	res := make([]*istio.TCPRoute, 0, len(routes))
	for _, r := range routes {
		r = r.DeepCopy()
		for _, m := range r.Match {
			m.Port = uint32(port)
		}
		if len(r.Match) == 0 {
			r.Match = []*istio.L4MatchAttributes{{
				Port: uint32(port),
			}}
		}
		res = append(res, r)
	}
	return res
}

func augmentTLSPortMatch(routes []*istio.TLSRoute, port *k8sbeta.PortNumber) ([]*istio.TLSRoute, []*istio.TCPRoute) {
	res := make([]*istio.TLSRoute, 0, len(routes))
	tcpRes := make([]*istio.TCPRoute, 0, len(routes))
	for _, r := range routes {
		if len(r.Match) == 1 && slices.Equal(r.Match[0].SniHosts, []string{"*"}) {
			// For mesh, we cannot set "*" on SNI. But we also cannot set SNI to the host, or we would match on SNI which we do
			// not want
			// Instead, turn it into a TCPRoute
			rt := &istio.TCPRoute{
				Match: nil,
				Route: r.Route,
			}
			if port != nil {
				rt.Match = []*istio.L4MatchAttributes{{
					Port: uint32(*port),
				}}
			}
			tcpRes = append(tcpRes, rt)
			continue
		}
		r = r.DeepCopy()
		for _, m := range r.Match {
			if port != nil {
				m.Port = uint32(*port)
			}
		}
		res = append(res, r)
	}
	return res, tcpRes
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
			return false
		} else if len(routes[j].Match) == 0 {
			return true
		}
		m1, m2 := routes[i].Match[0], routes[j].Match[0]
		r1, r2 := getURIRank(m1), getURIRank(m2)
		len1, len2 := getURILength(m1), getURILength(m2)
		if r1 == r2 {
			if len1 == len2 {
				if len(m1.Headers) == len(m2.Headers) {
					return len(m1.QueryParams) > len(m2.QueryParams)
				}
				return len(m1.Headers) > len(m2.Headers)
			}
			return len1 > len2
		}
		return r1 > r2
	})
}

// getURIRank ranks a URI match type. Exact > Prefix > Regex
func getURIRank(match *istio.HTTPMatchRequest) int {
	if match.Uri == nil {
		return -1
	}
	switch match.Uri.MatchType.(type) {
	case *istio.StringMatch_Exact:
		return 3
	case *istio.StringMatch_Prefix:
		return 2
	case *istio.StringMatch_Regex:
		return 1
	}
	// should not happen
	return -1
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
		constants.InternalParentNames: name,
	}
}

func hostnameToStringList(h []k8s.Hostname) []string {
	// In the Istio API, empty hostname is not allowed. In the Kubernetes API hosts means "any"
	if len(h) == 0 {
		return []string{"*"}
	}
	return slices.Map(h, func(e k8s.Hostname) string {
		return string(e)
	})
}

func toInternalParentReference(p k8s.ParentReference, localNamespace string) (parentKey, error) {
	empty := parentKey{}
	kind := ptr.OrDefault((*string)(p.Kind), gvk.KubernetesGateway.Kind)
	var ik config.GroupVersionKind
	var ns string
	// Currently supported types are Gateway and Service
	if kind == gvk.KubernetesGateway.Kind && nilOrEqual((*string)(p.Group), gvk.KubernetesGateway.Group) {
		ik = gvk.KubernetesGateway
	} else if kind == gvk.Service.Kind && (nilOrEqual((*string)(p.Group), gvk.Service.Group) ||
		*(*string)(p.Group) == gvk.KubernetesGateway.Group) { // TODO: gateway group is default?
		ik = gvk.Service
	} else {
		return empty, fmt.Errorf("unsupported parentKey: %v/%v", p.Group, kind)
	}
	// Unset namespace means "same namespace"
	ns = ptr.OrDefault((*string)(p.Namespace), localNamespace)
	return parentKey{
		Kind:      ik,
		Name:      string(p.Name),
		Namespace: ns,
	}, nil
}

func referenceAllowed(
	parent *parentInfo,
	routeKind config.GroupVersionKind,
	parentRef parentReference,
	hostnames []k8s.Hostname,
	namespace string,
) *ParentError {
	if parentRef.Kind == gvk.Service {
		// TODO: check if the service reference is valid
		if false {
			return &ParentError{
				Reason:  ParentErrorParentRefConflict,
				Message: fmt.Sprintf("parent service: %q is invalid", parentRef.Name),
			}
		}
	} else {
		// First, check section and port apply. This must come first
		if parentRef.Port != 0 && parentRef.Port != parent.Port {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("port %v not found", parentRef.Port),
			}
		}
		if len(parentRef.SectionName) > 0 && parentRef.SectionName != parent.SectionName {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("sectionName %q not found", parentRef.SectionName),
			}
		}

		// Next check the hostnames are a match. This is a bi-directional wildcard match. Only one route
		// hostname must match for it to be allowed (but the others will be filtered at runtime)
		// If either is empty its treated as a wildcard which always matches

		if len(hostnames) == 0 {
			hostnames = []k8s.Hostname{"*"}
		}
		if len(parent.Hostnames) > 0 {
			// TODO: the spec actually has a label match, not a string match. That is, *.com does not match *.apple.com
			// We are doing a string match here
			matched := false
			hostMatched := false
			for _, routeHostname := range hostnames {
				for _, parentHostNamespace := range parent.Hostnames {
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
					return &ParentError{
						Reason: ParentErrorNotAllowed,
						Message: fmt.Sprintf(
							"hostnames matched parent hostname %q, but namespace %q is not allowed by the parent",
							parent.OriginalHostname, namespace,
						),
					}
				}
				return &ParentError{
					Reason: ParentErrorNoHostname,
					Message: fmt.Sprintf(
						"no hostnames matched parent hostname %q",
						parent.OriginalHostname,
					),
				}
			}
		}
	}
	// Also make sure this route kind is allowed
	matched := false
	for _, ak := range parent.AllowedKinds {
		if string(ak.Kind) == routeKind.Kind && ptr.OrDefault((*string)(ak.Group), gvk.GatewayClass.Group) == routeKind.Group {
			matched = true
			break
		}
	}
	if !matched {
		return &ParentError{
			Reason:  ParentErrorNotAllowed,
			Message: fmt.Sprintf("kind %v is not allowed", routeKind),
		}
	}
	return nil
}

func extractParentReferenceInfo(gateways map[parentKey][]*parentInfo, routeRefs []k8s.ParentReference,
	hostnames []k8s.Hostname, kind config.GroupVersionKind, localNamespace string,
) []routeParentReference {
	parentRefs := []routeParentReference{}
	for _, ref := range routeRefs {
		ir, err := toInternalParentReference(ref, localNamespace)
		if err != nil {
			// Cannot handle the reference. Maybe it is for another controller, so we just ignore it
			continue
		}
		pk := parentReference{
			parentKey:   ir,
			SectionName: ptr.OrEmpty(ref.SectionName),
			Port:        ptr.OrEmpty(ref.Port),
		}
		appendParent := func(pr *parentInfo, pk parentReference) {
			rpi := routeParentReference{
				InternalName:      pr.InternalName,
				Hostname:          pr.OriginalHostname,
				DeniedReason:      referenceAllowed(pr, kind, pk, hostnames, localNamespace),
				OriginalReference: ref,
			}
			if rpi.DeniedReason == nil {
				// Record that we were able to bind to the parent
				pr.AttachedRoutes++
			}
			parentRefs = append(parentRefs, rpi)
		}
		gk := ir
		if ir.Kind == gvk.Service {
			gk = meshParentKey
		}
		for _, gw := range gateways[gk] {
			// Append all matches. Note we may be adding mismatch section or ports; this is handled later
			appendParent(gw, pk)
		}
	}
	// Ensure stable order
	slices.SortFunc(parentRefs, func(a, b routeParentReference) bool {
		return parentRefString(a.OriginalReference) < parentRefString(b.OriginalReference)
	})
	return parentRefs
}

func buildTCPVirtualService(ctx configContext, obj config.Config) []config.Config {
	route := obj.Spec.(*k8s.TCPRouteSpec)
	parentRefs := extractParentReferenceInfo(ctx.GatewayReferences, route.ParentRefs, nil, gvk.TCPRoute, obj.Namespace)

	reportStatus := func(results []RouteParentResult) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TCPRouteStatus)
			rs.Parents = createRouteStatus(results, obj, rs.Parents)
			return rs
		})
	}
	type conversionResult struct {
		error  *ConfigError
		routes []*istio.TCPRoute
	}
	convertRules := func(mesh bool) conversionResult {
		res := conversionResult{}
		for _, r := range route.Rules {
			vs, err := convertTCPRoute(ctx, r, obj, !mesh)
			// This was a hard error
			if vs == nil {
				res.error = err
				return conversionResult{error: err}
			}
			// Got an error but also routes
			if err != nil {
				res.error = err
			}
			res.routes = append(res.routes, vs)
		}
		return res
	}
	meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)
	reportStatus(slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
		res := RouteParentResult{
			OriginalReference: r.OriginalReference,
			DeniedReason:      r.DeniedReason,
			RouteError:        gwResult.error,
		}
		if r.IsMesh() {
			res.RouteError = meshResult.error
		}
		return res
	}))

	vs := []config.Config{}
	for _, parent := range filteredReferences(parentRefs) {
		routes := gwResult.routes
		vsHost := "*"
		if parent.IsMesh() {
			routes = meshResult.routes
			if parent.OriginalReference.Port != nil {
				routes = augmentTCPPortMatch(routes, *parent.OriginalReference.Port)
			}
			vsHost = fmt.Sprintf("%s.%s.svc.%s",
				parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, k8s.Namespace(obj.Namespace)), ctx.Domain)
		}
		vs = append(vs, config.Config{
			Meta: config.Meta{
				CreationTimestamp: obj.CreationTimestamp,
				GroupVersionKind:  gvk.VirtualService,
				Name:              fmt.Sprintf("%s-tcp-%s", obj.Name, constants.KubernetesGatewayName),
				Annotations:       routeMeta(obj),
				Namespace:         obj.Namespace,
				Domain:            ctx.Domain,
			},
			Spec: &istio.VirtualService{
				// We can use wildcard here since each listener can have at most one route bound to it, so we have
				// a single VS per Gateway.
				Hosts:    []string{vsHost},
				Gateways: []string{parent.InternalName},
				Tcp:      routes,
			},
		})
	}
	return vs
}

func buildTLSVirtualService(ctx configContext, obj config.Config) []config.Config {
	route := obj.Spec.(*k8s.TLSRouteSpec)
	parentRefs := extractParentReferenceInfo(ctx.GatewayReferences, route.ParentRefs, nil, gvk.TLSRoute, obj.Namespace)

	reportStatus := func(results []RouteParentResult) {
		obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
			rs := s.(*k8s.TLSRouteStatus)
			rs.Parents = createRouteStatus(results, obj, rs.Parents)
			return rs
		})
	}
	type conversionResult struct {
		error  *ConfigError
		routes []*istio.TLSRoute
	}
	convertRules := func(mesh bool) conversionResult {
		res := conversionResult{}
		for _, r := range route.Rules {
			vs, err := convertTLSRoute(ctx, r, obj, !mesh)
			// This was a hard error
			if vs == nil {
				res.error = err
				return conversionResult{error: err}
			}
			// Got an error but also routes
			if err != nil {
				res.error = err
			}
			res.routes = append(res.routes, vs)
		}
		return res
	}
	meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)
	reportStatus(slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
		res := RouteParentResult{
			OriginalReference: r.OriginalReference,
			DeniedReason:      r.DeniedReason,
			RouteError:        gwResult.error,
		}
		if r.IsMesh() {
			res.RouteError = meshResult.error
		}
		return res
	}))

	vs := []config.Config{}
	for _, parent := range filteredReferences(parentRefs) {
		routes, tcpRoutes := gwResult.routes, []*istio.TCPRoute{}
		vsHosts := hostnameToStringList(route.Hostnames)
		if parent.IsMesh() {
			routes = meshResult.routes
			routes, tcpRoutes = augmentTLSPortMatch(routes, parent.OriginalReference.Port)
			host := fmt.Sprintf("%s.%s.svc.%s",
				parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, k8s.Namespace(obj.Namespace)), ctx.Domain)
			vsHosts = []string{host}
		}

		for i, host := range vsHosts {
			name := fmt.Sprintf("%s-tls-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
			// Create one VS per hostname with a single hostname.
			// This ensures we can treat each hostname independently, as the spec requires
			vs = append(vs, config.Config{
				Meta: config.Meta{
					CreationTimestamp: obj.CreationTimestamp,
					GroupVersionKind:  gvk.VirtualService,
					Name:              name,
					Annotations:       routeMeta(obj),
					Namespace:         obj.Namespace,
					Domain:            ctx.Domain,
				},
				Spec: &istio.VirtualService{
					Hosts:    []string{host},
					Gateways: []string{parent.InternalName},
					// We cannot set both, but only one will be non empty
					Tls: routes,
					Tcp: tcpRoutes,
				},
			})
		}
	}
	return vs
}

func convertTCPRoute(ctx configContext, r k8s.TCPRouteRule, obj config.Config, enforceRefGrant bool) (*istio.TCPRoute, *ConfigError) {
	if tcpWeightSum(r.BackendRefs) == 0 {
		// The spec requires us to reject connections when there are no >0 weight backends
		// We don't have a great way to do it. TODO: add a fault injection API for TCP?
		return &istio.TCPRoute{
			Route: []*istio.RouteDestination{{
				Destination: &istio.Destination{
					Host:   "internal.cluster.local",
					Subset: "zero-weight",
					Port:   &istio.PortSelector{Number: 65535},
				},
				Weight: 0,
			}},
		}, nil
	}
	dest, backendErr, err := buildTCPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant)
	if err != nil {
		return nil, err
	}
	return &istio.TCPRoute{
		Route: dest,
	}, backendErr
}

func convertTLSRoute(ctx configContext, r k8s.TLSRouteRule, obj config.Config, enforceRefGrant bool) (*istio.TLSRoute, *ConfigError) {
	if tcpWeightSum(r.BackendRefs) == 0 {
		// The spec requires us to reject connections when there are no >0 weight backends
		// We don't have a great way to do it. TODO: add a fault injection API for TCP?
		return &istio.TLSRoute{
			Route: []*istio.RouteDestination{{
				Destination: &istio.Destination{
					Host:   "internal.cluster.local",
					Subset: "zero-weight",
					Port:   &istio.PortSelector{Number: 65535},
				},
				Weight: 0,
			}},
		}, nil
	}
	dest, backendErr, err := buildTCPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant)
	if err != nil {
		return nil, err
	}
	return &istio.TLSRoute{
		Match: buildTLSMatch(obj.Spec.(*k8s.TLSRouteSpec).Hostnames),
		Route: dest,
	}, backendErr
}

func buildTCPDestination(
	ctx configContext,
	forwardTo []k8s.BackendRef,
	ns string,
	enforceRefGrant bool,
) ([]*istio.RouteDestination, *ConfigError, *ConfigError) {
	if forwardTo == nil {
		return nil, nil, nil
	}

	weights := []int{}
	action := []k8s.BackendRef{}
	for _, w := range forwardTo {
		wt := int(ptr.OrDefault(w.Weight, 1))
		if wt == 0 {
			continue
		}
		action = append(action, w)
		weights = append(weights, wt)
	}
	if len(weights) == 1 {
		weights = []int{0}
	}

	var invalidBackendErr *ConfigError
	res := []*istio.RouteDestination{}
	for i, fwd := range action {
		dst, err := buildDestination(ctx, fwd, ns, enforceRefGrant)
		if err != nil {
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, nil, err
			}
		}
		res = append(res, &istio.RouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		})
	}
	return res, invalidBackendErr, nil
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

func weightSum(forwardTo []k8s.HTTPBackendRef) int {
	sum := int32(0)
	for _, w := range forwardTo {
		sum += ptr.OrDefault(w.Weight, 1)
	}
	return int(sum)
}

func tcpWeightSum(forwardTo []k8s.BackendRef) int {
	sum := int32(0)
	for _, w := range forwardTo {
		sum += ptr.OrDefault(w.Weight, 1)
	}
	return int(sum)
}

func buildHTTPDestination(
	ctx configContext,
	forwardTo []k8s.HTTPBackendRef,
	ns string,
	enforceRefGrant bool,
) ([]*istio.HTTPRouteDestination, *ConfigError, *ConfigError) {
	if forwardTo == nil {
		return nil, nil, nil
	}
	weights := []int{}
	action := []k8s.HTTPBackendRef{}
	for _, w := range forwardTo {
		wt := int(ptr.OrDefault(w.Weight, 1))
		if wt == 0 {
			continue
		}
		action = append(action, w)
		weights = append(weights, wt)
	}
	if len(weights) == 1 {
		weights = []int{0}
	}

	var invalidBackendErr *ConfigError
	res := []*istio.HTTPRouteDestination{}
	for i, fwd := range action {
		dst, err := buildDestination(ctx, fwd.BackendRef, ns, enforceRefGrant)
		if err != nil {
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, nil, err
			}
		}
		rd := &istio.HTTPRouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		}
		for _, filter := range fwd.Filters {
			switch filter.Type {
			case k8sbeta.HTTPRouteFilterRequestHeaderModifier:
				h := createHeadersFilter(filter.RequestHeaderModifier)
				if h == nil {
					continue
				}
				if rd.Headers == nil {
					rd.Headers = &istio.Headers{}
				}
				rd.Headers.Request = h
			case k8sbeta.HTTPRouteFilterResponseHeaderModifier:
				h := createHeadersFilter(filter.ResponseHeaderModifier)
				if h == nil {
					continue
				}
				if rd.Headers == nil {
					rd.Headers = &istio.Headers{}
				}
				rd.Headers.Response = h
			default:
				return nil, nil, &ConfigError{Reason: InvalidFilter, Message: fmt.Sprintf("unsupported filter type %q", filter.Type)}
			}
		}
		res = append(res, rd)
	}
	return res, invalidBackendErr, nil
}

func buildDestination(ctx configContext, to k8s.BackendRef, ns string, enforceRefGrant bool) (*istio.Destination, *ConfigError) {
	// check if the reference is allowed
	if enforceRefGrant {
		refs := ctx.AllowedReferences
		if toNs := to.Namespace; toNs != nil && string(*toNs) != ns {
			if !refs.BackendAllowed(gvk.HTTPRoute, to.Name, *toNs, ns) {
				return &istio.Destination{}, &ConfigError{
					Reason:  InvalidDestinationPermit,
					Message: fmt.Sprintf("backendRef %v/%v not accessible to a route in namespace %q (missing a ReferenceGrant?)", to.Name, *toNs, ns),
				}
			}
		}
	}

	namespace := ptr.OrDefault((*string)(to.Namespace), ns)
	var invalidBackendErr *ConfigError
	if nilOrEqual((*string)(to.Group), "") && nilOrEqual((*string)(to.Kind), gvk.Service.Kind) {
		// Service
		if to.Port == nil {
			// "Port is required when the referent is a Kubernetes Service."
			return nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
		}
		if strings.Contains(string(to.Name), ".") {
			return nil, &ConfigError{Reason: InvalidDestination, Message: "serviceName invalid; the name of the Service must be used, not the hostname."}
		}
		hostname := fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, ctx.Domain)
		if ctx.Context.GetService(hostname, namespace) == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
		return &istio.Destination{
			// TODO: implement ReferencePolicy for cross namespace
			Host: hostname,
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, invalidBackendErr
	}
	if nilOrEqual((*string)(to.Group), features.MCSAPIGroup) && nilOrEqual((*string)(to.Kind), "ServiceImport") {
		// Service import
		hostname := fmt.Sprintf("%s.%s.svc.clusterset.local", to.Name, namespace)
		if !features.EnableMCSHost {
			// They asked for ServiceImport, but actually don't have full support enabled...
			// No problem, we can just treat it as Service, which is already cross-cluster in this mode anyways
			hostname = fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, ctx.Domain)
		}
		if to.Port == nil {
			// We don't know where to send without port
			return nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
		}
		if strings.Contains(string(to.Name), ".") {
			return nil, &ConfigError{Reason: InvalidDestination, Message: "serviceName invalid; the name of the Service must be used, not the hostname."}
		}
		if ctx.Context.GetService(hostname, namespace) == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
		return &istio.Destination{
			Host: hostname,
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, invalidBackendErr
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
		hostname := string(to.Name)
		if ctx.Context.GetService(hostname, namespace) == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
		return &istio.Destination{
			Host: string(to.Name),
			Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, invalidBackendErr
	}
	return &istio.Destination{}, &ConfigError{
		Reason:  InvalidDestinationKind,
		Message: fmt.Sprintf("referencing unsupported backendRef: group %q kind %q", ptr.OrEmpty(to.Group), ptr.OrEmpty(to.Kind)),
	}
}

// https://github.com/kubernetes-sigs/gateway-api/blob/cea484e38e078a2c1997d8c7a62f410a1540f519/apis/v1beta1/httproute_types.go#L207-L212
func isInvalidBackend(err *ConfigError) bool {
	return err.Reason == InvalidDestinationPermit ||
		err.Reason == InvalidDestinationNotFound ||
		err.Reason == InvalidDestinationKind
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

func createMirrorFilter(ctx configContext, filter *k8s.HTTPRequestMirrorFilter, ns string, enforceRefGrant bool) (*istio.Destination, *ConfigError) {
	if filter == nil {
		return nil, nil
	}
	var weightOne int32 = 1
	return buildDestination(ctx, k8s.BackendRef{
		BackendObjectReference: filter.BackendRef,
		Weight:                 &weightOne,
	}, ns, enforceRefGrant)
}

func createRewriteFilter(filter *k8s.HTTPURLRewriteFilter) *istio.HTTPRewrite {
	if filter == nil {
		return nil
	}
	rewrite := &istio.HTTPRewrite{}
	if filter.Path != nil {
		switch filter.Path.Type {
		case k8sbeta.PrefixMatchHTTPPathModifier:
			rewrite.Uri = *filter.Path.ReplacePrefixMatch
		case k8sbeta.FullPathHTTPPathModifier:
			rewrite.UriRegexRewrite = &istio.RegexRewrite{
				Match:   "/.*",
				Rewrite: *filter.Path.ReplaceFullPath,
			}
		}
	}
	if filter.Hostname != nil {
		rewrite.Authority = string(*filter.Hostname)
	}
	// Nothing done
	if rewrite.Uri == "" && rewrite.UriRegexRewrite == nil && rewrite.Authority == "" {
		return nil
	}
	return rewrite
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
		if filter.Scheme != nil {
			resp.RedirectPort = &istio.HTTPRedirect_DerivePort{DerivePort: istio.HTTPRedirect_FROM_PROTOCOL_DEFAULT}
		} else {
			resp.RedirectPort = &istio.HTTPRedirect_DerivePort{DerivePort: istio.HTTPRedirect_FROM_REQUEST_PORT}
		}
	}
	if filter.Path != nil {
		switch filter.Path.Type {
		case k8sbeta.FullPathHTTPPathModifier:
			resp.Uri = *filter.Path.ReplaceFullPath
		case k8sbeta.PrefixMatchHTTPPathModifier:
			resp.Uri = fmt.Sprintf("%%PREFIX()%%%s", *filter.Path.ReplacePrefixMatch)
		}
	}
	return resp
}

func createHeadersFilter(filter *k8s.HTTPHeaderFilter) *istio.Headers_HeaderOperations {
	if filter == nil {
		return nil
	}
	return &istio.Headers_HeaderOperations{
		Add:    headerListToMap(filter.Add),
		Remove: filter.Remove,
		Set:    headerListToMap(filter.Set),
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
		tp := k8sbeta.QueryParamMatchExact
		if qp.Type != nil {
			tp = *qp.Type
		}
		switch tp {
		case k8sbeta.QueryParamMatchExact:
			res[string(qp.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: qp.Value},
			}
		case k8sbeta.QueryParamMatchRegularExpression:
			res[string(qp.Name)] = &istio.StringMatch{
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
		tp := k8sbeta.HeaderMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case k8sbeta.HeaderMatchExact:
			res[string(header.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: header.Value},
			}
		case k8sbeta.HeaderMatchRegularExpression:
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
	tp := k8sbeta.PathMatchPathPrefix
	if match.Path.Type != nil {
		tp = *match.Path.Type
	}
	dest := "/"
	if match.Path.Value != nil {
		dest = *match.Path.Value
	}
	switch tp {
	case k8sbeta.PathMatchPathPrefix:
		// "When specified, a trailing `/` is ignored."
		if dest != "/" {
			dest = strings.TrimSuffix(dest, "/")
		}
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Prefix{Prefix: dest},
		}, nil
	case k8sbeta.PathMatchExact:
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: dest},
		}, nil
	case k8sbeta.PathMatchRegularExpression:
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Regex{Regex: dest},
		}, nil
	default:
		// Should never happen, unless a new field is added
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported Path match type", tp)}
	}
}

// getGatewayClass finds all gateway class that are owned by Istio
// Response is ClassName -> Controller type
func getGatewayClasses(r GatewayResources) map[string]k8s.GatewayController {
	res := map[string]k8s.GatewayController{}
	allFound := sets.New[string]()
	for _, obj := range r.GatewayClass {
		gwc := obj.Spec.(*k8s.GatewayClassSpec)
		allFound.Insert(obj.Name)
		if gwc.ControllerName == constants.ManagedGatewayController ||
			features.EnableAmbientControllers && gwc.ControllerName == constants.ManagedGatewayMeshController {
			res[obj.Name] = gwc.ControllerName

			// Set status. If we created it, it may already be there. If not, set it again
			obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
				gcs := s.(*k8s.GatewayClassStatus)
				*gcs = GetClassStatus(gcs, obj.Generation)
				return gcs
			})
		}
	}
	if !allFound.Contains(defaultClassName) {
		// Allow `istio` class without explicit GatewayClass. However, if it already exists then do not
		// add it here, in case it points to a different controller.
		res[defaultClassName] = constants.ManagedGatewayController
	}
	if features.EnableAmbientControllers && !allFound.Contains(constants.WaypointGatewayClassName) {
		res[constants.WaypointGatewayClassName] = constants.ManagedGatewayMeshController
	}
	return res
}

// parentKey holds info about a parentRef (eg route binding to a Gateway). This is a mirror of
// k8s.ParentReference in a form that can be stored in a map
type parentKey struct {
	Kind config.GroupVersionKind
	// Name is the original name of the resource (eg Kubernetes Gateway name)
	Name string
	// Namespace is the namespace of the resource
	Namespace string
}

type parentReference struct {
	parentKey

	SectionName k8s.SectionName
	Port        k8sbeta.PortNumber
}

var meshGVK = config.GroupVersionKind{
	Group:   gvk.KubernetesGateway.Group,
	Version: gvk.KubernetesGateway.Version,
	Kind:    "Mesh",
}

var meshParentKey = parentKey{
	Kind: meshGVK,
	Name: "istio",
}

type configContext struct {
	GatewayResources
	AllowedReferences AllowedReferences
	GatewayReferences map[parentKey][]*parentInfo

	// key: referenced resources(e.g. secrets), value: gateway-api resources(e.g. gateways)
	resourceReferences map[model.ConfigKey][]model.ConfigKey
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
	SectionName          k8s.SectionName
	Port                 k8sbeta.PortNumber
}

// routeParentReference holds information about a route's parent reference
type routeParentReference struct {
	// InternalName refers to the internal name of the parent we can reference it by. For example, "mesh" or "my-ns/my-gateway"
	InternalName string
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
	// Hostname is the hostname match of the parent, if any
	Hostname string
}

func (r routeParentReference) IsMesh() bool {
	return r.InternalName == "mesh"
}

func filteredReferences(parents []routeParentReference) []routeParentReference {
	ret := make([]routeParentReference, 0, len(parents))
	for _, p := range parents {
		if p.DeniedReason != nil {
			// We should filter this out
			continue
		}
		ret = append(ret, p)
	}
	// To ensure deterministic order, sort them
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].InternalName < ret[j].InternalName
	})
	return ret
}

func getDefaultName(name string, kgw *k8s.GatewaySpec) string {
	return fmt.Sprintf("%v-%v", name, kgw.GatewayClassName)
}

func convertGateways(r configContext) ([]config.Config, map[parentKey][]*parentInfo, sets.String) {
	// result stores our generated Istio Gateways
	result := []config.Config{}
	// gwMap stores an index to access parentInfo (which corresponds to a Kubernetes Gateway)
	gwMap := map[parentKey][]*parentInfo{}
	// namespaceLabelReferences keeps track of all namespace label keys referenced by Gateways. This is
	// used to ensure we handle namespace updates for those keys.
	namespaceLabelReferences := sets.New[string]()
	classes := getGatewayClasses(r.GatewayResources)
	for _, obj := range r.Gateway {
		obj := obj
		kgw := obj.Spec.(*k8s.GatewaySpec)
		controllerName, f := classes[string(kgw.GatewayClassName)]
		if !f {
			// No gateway class found, this may be meant for another controller; should be skipped.
			continue
		}

		servers := []*istio.Server{}

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, skippedAddresses := extractGatewayServices(r.GatewayResources, kgw, obj)
		for i, l := range kgw.Listeners {
			i := i
			namespaceLabelReferences.InsertAll(getNamespaceLabelReferences(l.AllowedRoutes)...)
			server, ok := buildListener(r, obj, l, i, controllerName)
			if !ok {
				continue
			}
			servers = append(servers, server)
			if controllerName == constants.ManagedGatewayMeshController {
				// Waypoint doesn't actually convert the routes to VirtualServices
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
				gwMap[ref] = []*parentInfo{}
			}

			allowed, _ := generateSupportedKinds(l)
			pri := &parentInfo{
				InternalName:     obj.Namespace + "/" + gatewayConfig.Name,
				AllowedKinds:     allowed,
				Hostnames:        server.Hosts,
				OriginalHostname: string(ptr.OrEmpty(l.Hostname)),
				SectionName:      l.Name,
				Port:             l.Port,
			}
			pri.ReportAttachedRoutes = func() {
				reportListenerAttachedRoutes(i, obj, pri.AttachedRoutes)
			}
			gwMap[ref] = append(gwMap[ref], pri)
			result = append(result, gatewayConfig)
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

		reportGatewayStatus(r, obj, gatewayServices, servers, skippedAddresses)
	}
	// Insert a parent for Mesh references.
	gwMap[meshParentKey] = []*parentInfo{
		{
			InternalName: "mesh",
			// Mesh has no configurable AllowedKinds, so allow all supported
			AllowedKinds: []k8s.RouteGroupKind{
				{Group: (*k8s.Group)(ptr.Of(gvk.HTTPRoute.Group)), Kind: k8s.Kind(gvk.HTTPRoute.Kind)},
				{Group: (*k8s.Group)(ptr.Of(gvk.TCPRoute.Group)), Kind: k8s.Kind(gvk.TCPRoute.Kind)},
				{Group: (*k8s.Group)(ptr.Of(gvk.TLSRoute.Group)), Kind: k8s.Kind(gvk.TLSRoute.Kind)},
			},
		},
	}
	return result, gwMap, namespaceLabelReferences
}

// Gateway currently requires a listener (https://github.com/kubernetes-sigs/gateway-api/pull/1596).
// We don't *really* care about the listener, but it may make sense to add a warning if users do not
// configure it in an expected way so that we have consistency and can make changes in the future as needed.
// We could completely reject but that seems more likely to cause pain.
func unexpectedWaypointListener(l k8s.Listener) bool {
	if l.Port != 15008 {
		return true
	}
	if l.Protocol != k8s.ProtocolType(protocol.HBONE) {
		return true
	}
	return false
}

func getListenerNames(obj config.Config) sets.Set[k8s.SectionName] {
	res := sets.New[k8s.SectionName]()
	for _, l := range obj.Spec.(*k8s.GatewaySpec).Listeners {
		res.Insert(l.Name)
	}
	return res
}

func reportGatewayStatus(
	r configContext,
	obj config.Config,
	gatewayServices []string,
	servers []*istio.Server,
	skippedAddresses []string,
) {
	// TODO: we lose address if servers is empty due to an error
	internal, external, pending, warnings := r.Context.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)

	if len(skippedAddresses) > 0 {
		warnings = append(warnings, fmt.Sprintf("Only Hostname is supported, ignoring %v", skippedAddresses))
	}

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(k8sbeta.GatewayConditionAccepted): {
			reason:  string(k8sbeta.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(k8sbeta.GatewayConditionProgrammed): {
			reason:  string(k8sbeta.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if len(internal) > 0 {
		msg := fmt.Sprintf("Resource programmed, assigned to service(s) %s", humanReadableJoin(internal))
		gatewayConditions[string(k8sbeta.GatewayReasonProgrammed)].message = msg
	}

	if len(warnings) > 0 {
		var msg string
		if len(internal) != 0 {
			msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
				humanReadableJoin(internal), strings.Join(warnings, "; "))
		} else {
			msg = fmt.Sprintf("Failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
		}
		gatewayConditions[string(k8sbeta.GatewayConditionProgrammed)].error = &ConfigError{
			// TODO(https://github.com/kubernetes-sigs/gateway-api/issues/1832#issuecomment-1487167378): Invalid is bad,
			// this should be AddressNotAssigned
			// TODO: this only checks Service ready, we should also check Deployment ready?
			Reason:  string(k8sbeta.GatewayReasonInvalid),
			Message: msg,
		}
	}
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		addressesToReport := external
		addrType := k8s.IPAddressType
		if len(addressesToReport) == 0 {
			// There are no external addresses, so report the internal ones
			// TODO: should we always report both?
			addrType = k8s.HostnameAddressType
			for _, hostport := range internal {
				svchost, _, _ := net.SplitHostPort(hostport)
				if !contains(pending, svchost) && !contains(addressesToReport, svchost) {
					addressesToReport = append(addressesToReport, svchost)
				}
			}
		}
		gs.Addresses = make([]k8s.GatewayAddress, 0, len(addressesToReport))
		for _, addr := range addressesToReport {
			gs.Addresses = append(gs.Addresses, k8s.GatewayAddress{
				Type:  &addrType,
				Value: addr,
			})
		}
		// Prune listeners that have been removed
		haveListeners := getListenerNames(obj)
		listeners := make([]k8s.ListenerStatus, 0, len(gs.Listeners))
		for _, l := range gs.Listeners {
			if haveListeners.Contains(l.Name) {
				haveListeners.Delete(l.Name)
				listeners = append(listeners, l)
			}
		}
		gs.Listeners = listeners
		gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
		return gs
	})
}

// IsManaged checks if a Gateway is managed (ie we create the Deployment and Service) or unmanaged.
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
//
//	which users can add to the Gateway.
func IsManaged(gw *k8s.GatewaySpec) bool {
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

func extractGatewayServices(r GatewayResources, kgw *k8s.GatewaySpec, obj config.Config) ([]string, []string) {
	if IsManaged(kgw) {
		name := model.GetOrDefault(obj.Annotations[gatewayNameOverride], getDefaultName(obj.Name, kgw))
		return []string{fmt.Sprintf("%s.%s.svc.%v", name, obj.Namespace, r.Domain)}, nil
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

func buildListener(r configContext, obj config.Config, l k8s.Listener, listenerIndex int, controllerName k8s.GatewayController) (*istio.Server, bool) {
	listenerConditions := map[string]*condition{
		string(k8sbeta.ListenerConditionAccepted): {
			reason:  string(k8sbeta.ListenerReasonAccepted),
			message: "No errors found",
		},
		string(k8sbeta.ListenerConditionProgrammed): {
			reason:  string(k8sbeta.ListenerReasonProgrammed),
			message: "No errors found",
		},
		string(k8sbeta.ListenerConditionConflicted): {
			reason:  string(k8sbeta.ListenerReasonNoConflicts),
			message: "No errors found",
			status:  kstatus.StatusFalse,
		},
		string(k8sbeta.ListenerConditionResolvedRefs): {
			reason:  string(k8sbeta.ListenerReasonResolvedRefs),
			message: "No errors found",
		},
	}

	defer reportListenerCondition(listenerIndex, l, obj, listenerConditions)

	tls, err := buildTLS(r, l.TLS, obj, isAutoPassthrough(obj, l))
	if err != nil {
		listenerConditions[string(k8sbeta.ListenerConditionResolvedRefs)].error = err
		return nil, false
	}
	hostnames := buildHostnameMatch(obj.Namespace, r.GatewayResources, l)
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
	if controllerName == constants.ManagedGatewayMeshController {
		if unexpectedWaypointListener(l) {
			listenerConditions[string(k8sbeta.ListenerConditionAccepted)].error = &ConfigError{
				Reason:  string(k8sbeta.ListenerReasonUnsupportedProtocol),
				Message: `Expected a single listener on port 15008 with protocol "HBONE"`,
			}
		}
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

	if port, f := obj.Labels[label.NetworkingGatewayPort.Name]; f {
		expectedPort = port
	}
	return fmt.Sprint(l.Port) == expectedPort
}

func listenerProtocolToIstio(protocol k8s.ProtocolType) string {
	// Currently, all gateway-api protocols are valid Istio protocols.
	return string(protocol)
}

func buildTLS(ctx configContext, tls *k8s.GatewayTLSConfig, gw config.Config, isAutoPassthrough bool) (*istio.ServerTLSSettings, *ConfigError) {
	if tls == nil {
		return nil, nil
	}
	// Explicitly not supported: file mounted
	// Not yet implemented: TLS mode, https redirect, max protocol version, SANs, CipherSuites, VerifyCertificate
	out := &istio.ServerTLSSettings{
		HttpsRedirect: false,
	}
	mode := k8sbeta.TLSModeTerminate
	if tls.Mode != nil {
		mode = *tls.Mode
	}
	namespace := gw.Namespace
	switch mode {
	case k8sbeta.TLSModeTerminate:
		out.Mode = istio.ServerTLSSettings_SIMPLE
		if tls.Options != nil && tls.Options[gatewayTLSTerminateModeKey] == "MUTUAL" {
			out.Mode = istio.ServerTLSSettings_MUTUAL
		}
		if len(tls.CertificateRefs) != 1 {
			// This is required in the API, should be rejected in validation
			return nil, &ConfigError{Reason: InvalidTLS, Message: "exactly 1 certificateRefs should be present for TLS termination"}
		}
		cred, err := buildSecretReference(ctx, tls.CertificateRefs[0], gw)
		if err != nil {
			return nil, err
		}
		credNs := ptr.OrDefault((*string)(tls.CertificateRefs[0].Namespace), namespace)
		sameNamespace := credNs == namespace
		if !sameNamespace && !ctx.AllowedReferences.SecretAllowed(creds.ToResourceName(cred), namespace) {
			return nil, &ConfigError{
				Reason: InvalidListenerRefNotPermitted,
				Message: fmt.Sprintf(
					"certificateRef %v/%v not accessible to a Gateway in namespace %q (missing a ReferenceGrant?)",
					tls.CertificateRefs[0].Name, credNs, namespace,
				),
			}
		}
		out.CredentialName = cred
	case k8sbeta.TLSModePassthrough:
		out.Mode = istio.ServerTLSSettings_PASSTHROUGH
		if isAutoPassthrough {
			out.Mode = istio.ServerTLSSettings_AUTO_PASSTHROUGH
		}
	}
	return out, nil
}

func buildSecretReference(ctx configContext, ref k8s.SecretObjectReference, gw config.Config) (string, *ConfigError) {
	if !nilOrEqual((*string)(ref.Group), gvk.Secret.Group) || !nilOrEqual((*string)(ref.Kind), gvk.Secret.Kind) {
		return "", &ConfigError{Reason: InvalidTLS, Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", objectReferenceString(ref))}
	}

	secret := model.ConfigKey{
		Kind:      kind.Secret,
		Name:      string(ref.Name),
		Namespace: ptr.OrDefault((*string)(ref.Namespace), gw.Namespace),
	}

	ctx.resourceReferences[secret] = append(ctx.resourceReferences[secret], model.ConfigKey{
		Kind:      kind.KubernetesGateway,
		Namespace: gw.Namespace,
		Name:      gw.Name,
	})

	if ctx.Credentials != nil {
		if certInfo, err := ctx.Credentials.GetCertInfo(secret.Name, secret.Namespace); err != nil {
			return "", &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid certificate reference %v, %v", objectReferenceString(ref), err),
			}
		} else if _, err = tls.X509KeyPair(certInfo.Cert, certInfo.Key); err != nil {
			return "", &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid certificate reference %v, the certificate is malformed: %v", objectReferenceString(ref), err),
			}
		}
	}

	return creds.ToKubernetesGatewayResource(secret.Namespace, secret.Name), nil
}

func objectReferenceString(ref k8s.SecretObjectReference) string {
	return fmt.Sprintf("%s/%s/%s.%s",
		ptr.OrEmpty(ref.Group),
		ptr.OrEmpty(ref.Kind),
		ref.Name,
		ptr.OrEmpty(ref.Namespace))
}

func parentRefString(ref k8s.ParentReference) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d.%s",
		ptr.OrEmpty(ref.Group),
		ptr.OrEmpty(ref.Kind),
		ref.Name,
		ptr.OrEmpty(ref.SectionName),
		ptr.OrEmpty(ref.Port),
		ptr.OrEmpty(ref.Namespace))
}

// buildHostnameMatch generates a VirtualService.spec.hosts section from a listener
func buildHostnameMatch(localNamespace string, r GatewayResources, l k8s.Listener) []string {
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
func namespacesFromSelector(localNamespace string, r GatewayResources, lr *k8s.AllowedRoutes) []string {
	// Default is to allow only the same namespace
	if lr == nil || lr.Namespaces == nil || lr.Namespaces.From == nil || *lr.Namespaces.From == k8sbeta.NamespacesFromSame {
		return []string{localNamespace}
	}
	if *lr.Namespaces.From == k8sbeta.NamespacesFromAll {
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

func nilOrEqual(have *string, expected string) bool {
	return have == nil || *have == expected
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

func contains(ss []string, s string) bool {
	for _, str := range ss {
		if str == s {
			return true
		}
	}
	return false
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

func (kr GatewayResources) FuzzValidate() bool {
	for _, gwc := range kr.GatewayClass {
		if gwc.Spec == nil {
			return false
		}
	}
	for _, rp := range kr.ReferenceGrant {
		if rp.Spec == nil {
			return false
		}
	}
	for _, hr := range kr.HTTPRoute {
		if hr.Spec == nil {
			return false
		}
	}
	for _, tr := range kr.TLSRoute {
		if tr.Spec == nil {
			return false
		}
	}
	for _, g := range kr.Gateway {
		if g.Spec == nil {
			return false
		}
	}
	for _, tr := range kr.TCPRoute {
		if tr.Spec == nil {
			return false
		}
	}
	return true
}
