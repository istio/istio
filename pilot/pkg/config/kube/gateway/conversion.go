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
	"cmp"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	k8s "sigs.k8s.io/gateway-api/apis/v1"
	k8salpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	istio "istio.io/api/networking/v1alpha3"
	kubecreds "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const (
	gatewayTLSTerminateModeKey = "gateway.istio.io/tls-terminate-mode"
	addressTypeOverride        = "networking.istio.io/address-type"
	gatewayClassDefaults       = "gateway.istio.io/defaults-for-class"
)

func sortConfigByCreationTime(configs []config.Config) {
	sort.Slice(configs, func(i, j int) bool {
		if r := configs[i].CreationTimestamp.Compare(configs[j].CreationTimestamp); r != 0 {
			return r == -1 // -1 means i is less than j, so return true
		}
		if r := cmp.Compare(configs[i].Namespace, configs[j].Namespace); r != 0 {
			return r == -1
		}
		return cmp.Compare(configs[i].Name, configs[j].Name) == -1
	})
}

func sortRoutesByCreationTime(configs []RouteWithKey) {
	sort.Slice(configs, func(i, j int) bool {
		if r := configs[i].CreationTimestamp.Compare(configs[j].CreationTimestamp); r != 0 {
			return r == -1 // -1 means i is less than j, so return true
		}
		if r := cmp.Compare(configs[i].Namespace, configs[j].Namespace); r != 0 {
			return r == -1
		}
		return cmp.Compare(configs[i].Name, configs[j].Name) == -1
	})
}

func sortedConfigByCreationTime(configs []config.Config) []config.Config {
	sortConfigByCreationTime(configs)
	return configs
}

func convertHTTPRoute(ctx RouteContext, r k8s.HTTPRouteRule,
	obj *k8sbeta.HTTPRoute, pos int, enforceRefGrant bool,
) (*istio.HTTPRoute, *inferencePoolConfig, *ConfigError) {
	vs := &istio.HTTPRoute{}
	if r.Name != nil {
		vs.Name = string(*r.Name)
	} else {
		// Auto-name the route. If upstream defines an explicit name, will use it instead
		// The position within the route is unique
		vs.Name = obj.Namespace + "." + obj.Name + "." + strconv.Itoa(pos) // format: %s.%s.%d
	}

	for _, match := range r.Matches {
		uri, err := createURIMatch(match)
		if err != nil {
			return nil, nil, err
		}
		headers, err := createHeadersMatch(match)
		if err != nil {
			return nil, nil, err
		}
		qp, err := createQueryParamsMatch(match)
		if err != nil {
			return nil, nil, err
		}
		method, err := createMethodMatch(match)
		if err != nil {
			return nil, nil, err
		}
		vs.Match = append(vs.Match, &istio.HTTPMatchRequest{
			Uri:         uri,
			Headers:     headers,
			QueryParams: qp,
			Method:      method,
		})
	}
	var mirrorBackendErr *ConfigError
	for _, filter := range r.Filters {
		switch filter.Type {
		case k8s.HTTPRouteFilterRequestHeaderModifier:
			h := createHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Request = h
		case k8s.HTTPRouteFilterResponseHeaderModifier:
			h := createHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Response = h
		case k8s.HTTPRouteFilterRequestRedirect:
			vs.Redirect = createRedirectFilter(filter.RequestRedirect)
		case k8s.HTTPRouteFilterRequestMirror:
			mirror, err := createMirrorFilter(ctx, filter.RequestMirror, obj.Namespace, enforceRefGrant, gvk.HTTPRoute)
			if err != nil {
				mirrorBackendErr = err
			} else {
				vs.Mirrors = append(vs.Mirrors, mirror)
			}
		case k8s.HTTPRouteFilterURLRewrite:
			vs.Rewrite = createRewriteFilter(filter.URLRewrite)
		case k8s.HTTPRouteFilterCORS:
			vs.CorsPolicy = createCorsFilter(filter.CORS)
		default:
			return nil, nil, &ConfigError{
				Reason:  InvalidFilter,
				Message: fmt.Sprintf("unsupported filter type %q", filter.Type),
			}
		}
	}

	if r.Retry != nil {
		// "Implementations SHOULD retry on connection errors (disconnect, reset, timeout,
		// TCP failure) if a retry stanza is configured."
		retryOn := []string{"connect-failure", "refused-stream", "unavailable", "cancelled"}
		for _, codes := range r.Retry.Codes {
			retryOn = append(retryOn, strconv.Itoa(int(codes)))
		}
		vs.Retries = &istio.HTTPRetry{
			// If unset, default is implementation specific.
			// VirtualService.retry has no default when set -- users are expected to set it if they customize `retry`.
			// However, the default retry if none are set is "2", so we use that as the default.
			Attempts:      int32(ptr.OrDefault(r.Retry.Attempts, 2)),
			PerTryTimeout: nil,
			RetryOn:       strings.Join(retryOn, ","),
		}
		if vs.Retries.Attempts == 0 {
			// Invalid to set this when there are no attempts
			vs.Retries.RetryOn = ""
		}
		if r.Retry.Backoff != nil {
			retrybackOff, _ := time.ParseDuration(string(*r.Retry.Backoff))
			vs.Retries.Backoff = durationpb.New(retrybackOff)
		}
	}

	if r.Timeouts != nil {
		if r.Timeouts.Request != nil {
			request, _ := time.ParseDuration(string(*r.Timeouts.Request))
			if request != 0 {
				vs.Timeout = durationpb.New(request)
			}
		}
		if r.Timeouts.BackendRequest != nil {
			backendRequest, _ := time.ParseDuration(string(*r.Timeouts.BackendRequest))
			if backendRequest != 0 {
				timeout := durationpb.New(backendRequest)
				if vs.Retries != nil {
					vs.Retries.PerTryTimeout = timeout
				} else {
					vs.Timeout = timeout
				}
			}
		}
	}
	if weightSum(r.BackendRefs) == 0 && vs.Redirect == nil {
		// The spec requires us to return 500 when there are no >0 weight backends
		vs.DirectResponse = &istio.HTTPDirectResponse{
			Status: 500,
		}
	} else {
		route, ipCfg, backendErr, err := buildHTTPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant)
		if err != nil {
			return nil, nil, err
		}
		vs.Route = route
		return vs, ipCfg, joinErrors(backendErr, mirrorBackendErr)
	}

	return vs, nil, mirrorBackendErr
}

func joinErrors(a *ConfigError, b *ConfigError) *ConfigError {
	if b == nil {
		return a
	}
	if a == nil {
		return b
	}
	a.Message += "; " + b.Message
	return a
}

func convertGRPCRoute(ctx RouteContext, r k8s.GRPCRouteRule,
	obj *k8s.GRPCRoute, pos int, enforceRefGrant bool,
) (*istio.HTTPRoute, *ConfigError) { // Assuming GRPCRoute doesn't need inferencePoolConfig for now
	vs := &istio.HTTPRoute{}
	if r.Name != nil {
		vs.Name = string(*r.Name)
	} else {
		// Auto-name the route. If upstream defines an explicit name, will use it instead
		// The position within the route is unique
		vs.Name = obj.Namespace + "." + obj.Name + "." + strconv.Itoa(pos) // format:%s.%s.%d
	}

	for _, match := range r.Matches {
		uri, err := createGRPCURIMatch(match)
		if err != nil {
			return nil, err
		}
		headers, err := createGRPCHeadersMatch(match)
		if err != nil {
			return nil, err
		}
		vs.Match = append(vs.Match, &istio.HTTPMatchRequest{
			Uri:     uri,
			Headers: headers,
		})
	}
	for _, filter := range r.Filters {
		switch filter.Type {
		case k8s.GRPCRouteFilterRequestHeaderModifier:
			h := createHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Request = h
		case k8s.GRPCRouteFilterResponseHeaderModifier:
			h := createHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			if vs.Headers == nil {
				vs.Headers = &istio.Headers{}
			}
			vs.Headers.Response = h
		case k8s.GRPCRouteFilterRequestMirror:
			mirror, err := createMirrorFilter(ctx, filter.RequestMirror, obj.Namespace, enforceRefGrant, gvk.GRPCRoute)
			if err != nil {
				return nil, err
			}
			vs.Mirrors = append(vs.Mirrors, mirror)
		default:
			return nil, &ConfigError{
				Reason:  InvalidFilter,
				Message: fmt.Sprintf("unsupported filter type %q", filter.Type),
			}
		}
	}

	if grpcWeightSum(r.BackendRefs) == 0 && vs.Redirect == nil {
		// The spec requires us to return 500 when there are no >0 weight backends
		vs.DirectResponse = &istio.HTTPDirectResponse{
			Status: 500,
		}
	} else {
		route, backendErr, err := buildGRPCDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant)
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

func augmentPortMatch(routes []*istio.HTTPRoute, port k8s.PortNumber) []*istio.HTTPRoute {
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

func augmentTCPPortMatch(routes []*istio.TCPRoute, port k8s.PortNumber) []*istio.TCPRoute {
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

func augmentTLSPortMatch(routes []*istio.TLSRoute, port *k8s.PortNumber, parentHosts []string) []*istio.TLSRoute {
	res := make([]*istio.TLSRoute, 0, len(routes))
	for _, r := range routes {
		r = r.DeepCopy()
		if len(r.Match) == 1 && slices.Equal(r.Match[0].SniHosts, []string{"*"}) {
			// For mesh, we use parent hosts for SNI if TLSRroute.hostnames were not specified.
			r.Match[0].SniHosts = parentHosts
		}
		for _, m := range r.Match {
			if port != nil {
				m.Port = uint32(*port)
			}
		}
		res = append(res, r)
	}
	return res
}

func compatibleRoutesForHost(routes []*istio.TLSRoute, parentHost string) []*istio.TLSRoute {
	res := make([]*istio.TLSRoute, 0, len(routes))
	for _, r := range routes {
		if len(r.Match) == 1 && len(r.Match[0].SniHosts) > 1 {
			r = r.DeepCopy()
			sniHosts := []string{}
			for _, h := range r.Match[0].SniHosts {
				if host.Name(parentHost).Matches(host.Name(h)) {
					sniHosts = append(sniHosts, h)
				}
			}
			r.Match[0].SniHosts = sniHosts
		}
		res = append(res, r)
	}
	return res
}

func routeMeta(obj controllers.Object) map[string]string {
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
		// Only look at match[0], we always generate only one match
		m1, m2 := routes[i].Match[0], routes[j].Match[0]
		r1, r2 := getURIRank(m1), getURIRank(m2)
		len1, len2 := getURILength(m1), getURILength(m2)
		switch {
		// 1: Exact/Prefix/Regex
		case r1 != r2:
			return r1 > r2
		case len1 != len2:
			return len1 > len2
			// 2: method math
		case (m1.Method == nil) != (m2.Method == nil):
			return m1.Method != nil
			// 3: number of header matches
		case len(m1.Headers) != len(m2.Headers):
			return len(m1.Headers) > len(m2.Headers)
			// 4: number of query matches
		default:
			return len(m1.QueryParams) > len(m2.QueryParams)
		}
	})
}

func parentMeta(obj controllers.Object, sectionName *k8s.SectionName) map[string]string {
	name := fmt.Sprintf("%s/%s.%s", schematypes.GvkFromObject(obj).Kind, obj.GetName(), obj.GetNamespace())
	if sectionName != nil {
		name = fmt.Sprintf("%s/%s/%s.%s", schematypes.GvkFromObject(obj).Kind, obj.GetName(), *sectionName, obj.GetNamespace())
	}
	return map[string]string{
		constants.InternalParentNames: name,
	}
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

func hostnameToStringList(h []k8s.Hostname) []string {
	// In the Istio API, empty hostname is not allowed. In the Kubernetes API hosts means "any"
	if len(h) == 0 {
		return []string{"*"}
	}
	return slices.Map(h, func(e k8s.Hostname) string {
		return string(e)
	})
}

var allowedParentReferences = sets.New(
	gvk.KubernetesGateway,
	gvk.Service,
	gvk.ServiceEntry,
	gvk.XListenerSet,
)

func toInternalParentReference(p k8s.ParentReference, localNamespace string) (parentKey, error) {
	ref := normalizeReference(p.Group, p.Kind, gvk.KubernetesGateway)
	if !allowedParentReferences.Contains(ref) {
		return parentKey{}, fmt.Errorf("unsupported parent: %v/%v", p.Group, p.Kind)
	}
	return parentKey{
		Kind: ref,
		Name: string(p.Name),
		// Unset namespace means "same namespace"
		Namespace: defaultString(p.Namespace, localNamespace),
	}, nil
}

// waypointConfigured returns true if a waypoint is configured via expected label's key-value pair.
func waypointConfigured(labels map[string]string) bool {
	if val, ok := labels[label.IoIstioUseWaypoint.Name]; ok && len(val) > 0 && !strings.EqualFold(val, "none") {
		return true
	}
	return false
}

func referenceAllowed(
	ctx RouteContext,
	parent *parentInfo,
	routeKind config.GroupVersionKind,
	parentRef parentReference,
	hostnames []k8s.Hostname,
	localNamespace string,
) (*ParentError, *WaypointError) {
	if parentRef.Kind == gvk.Service {

		key := parentRef.Namespace + "/" + parentRef.Name
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))

		// check that the referenced svc exists
		if svc == nil {
			return &ParentError{
					Reason:  ParentErrorNotAccepted,
					Message: fmt.Sprintf("parent service: %q not found", parentRef.Name),
				}, &WaypointError{
					Reason:  WaypointErrorReasonNoMatchingParent,
					Message: WaypointErrorMsgNoMatchingParent,
				}
		}

		// check that the reference has the use-waypoint label
		if !waypointConfigured(svc.Labels) {
			// if reference does not have use-waypoint label, check the namespace of the reference
			ns := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Namespaces, krt.FilterKey(svc.Namespace)))
			if ns != nil {
				if !waypointConfigured(ns.Labels) {
					return nil, &WaypointError{
						Reason:  WaypointErrorReasonMissingLabel,
						Message: WaypointErrorMsgMissingLabel,
					}
				}
			}
		}
	} else if parentRef.Kind == gvk.ServiceEntry {
		// check that the referenced svc entry exists
		key := parentRef.Namespace + "/" + parentRef.Name
		svcEntry := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(key)))
		if svcEntry == nil {
			return &ParentError{
					Reason:  ParentErrorNotAccepted,
					Message: fmt.Sprintf("parent service entry: %q not found", parentRef.Name),
				}, &WaypointError{
					Reason:  WaypointErrorReasonNoMatchingParent,
					Message: WaypointErrorMsgNoMatchingParent,
				}
		}

		// check that the reference has the use-waypoint label
		if !waypointConfigured(svcEntry.Labels) {
			// if reference does not have use-waypoint label, check the namespace of the reference
			ns := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Namespaces, krt.FilterKey(parentRef.Namespace)))
			if ns != nil {
				if !waypointConfigured(ns.Labels) {
					return nil, &WaypointError{
						Reason:  WaypointErrorReasonMissingLabel,
						Message: WaypointErrorMsgMissingLabel,
					}
				}
			}
		}
	} else {
		// First, check section and port apply. This must come first
		if parentRef.Port != 0 && parentRef.Port != parent.Port {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("port %v not found", parentRef.Port),
			}, nil
		}
		if len(parentRef.SectionName) > 0 && parentRef.SectionName != parent.SectionName {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("sectionName %q not found", parentRef.SectionName),
			}, nil
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
		out:
			for _, routeHostname := range hostnames {
				for _, parentHostNamespace := range parent.Hostnames {
					var parentNamespace, parentHostname string
					// When parentHostNamespace lacks a '/', it was likely sanitized from '*/host' to 'host'
					// by sanitizeServerHostNamespace. Set parentNamespace to '*' to reflect the wildcard namespace
					// and parentHostname to the sanitized host to prevent an index out of range panic.
					if strings.Contains(parentHostNamespace, "/") {
						spl := strings.Split(parentHostNamespace, "/")
						parentNamespace, parentHostname = spl[0], spl[1]
					} else {
						parentNamespace, parentHostname = "*", parentHostNamespace
					}

					hostnameMatch := host.Name(parentHostname).Matches(host.Name(routeHostname))
					namespaceMatch := parentNamespace == "*" || parentNamespace == localNamespace
					hostMatched = hostMatched || hostnameMatch
					if hostnameMatch && namespaceMatch {
						matched = true
						break out
					}
				}
			}
			if !matched {
				if hostMatched {
					return &ParentError{
						Reason: ParentErrorNotAllowed,
						Message: fmt.Sprintf(
							"hostnames matched parent hostname %q, but namespace %q is not allowed by the parent",
							parent.OriginalHostname, localNamespace,
						),
					}, nil
				}
				return &ParentError{
					Reason: ParentErrorNoHostname,
					Message: fmt.Sprintf(
						"no hostnames matched parent hostname %q",
						parent.OriginalHostname,
					),
				}, nil
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
		}, nil
	}
	return nil, nil
}

func extractParentReferenceInfo(ctx RouteContext, parents RouteParents, obj controllers.Object) []routeParentReference {
	routeRefs, hostnames, kind := GetCommonRouteInfo(obj)
	localNamespace := obj.GetNamespace()
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
		gk := ir
		if ir.Kind == gvk.Service || ir.Kind == gvk.ServiceEntry {
			gk = meshParentKey
		}
		currentParents := parents.fetch(ctx.Krt, gk)
		appendParent := func(pr *parentInfo, pk parentReference) {
			bannedHostnames := sets.New[string]()
			for _, gw := range currentParents {
				if gw == pr {
					continue // do not ban ourself
				}
				if gw.Port != pr.Port {
					// We only care about listeners on the same port
					continue
				}
				if gw.Protocol != pr.Protocol {
					// We only care about listeners on the same protocol
					continue
				}
				bannedHostnames.Insert(gw.OriginalHostname)
			}
			deniedReason, waypointError := referenceAllowed(ctx, pr, kind, pk, hostnames, localNamespace)
			rpi := routeParentReference{
				InternalName:      pr.InternalName,
				InternalKind:      ir.Kind,
				Hostname:          pr.OriginalHostname,
				DeniedReason:      deniedReason,
				OriginalReference: ref,
				BannedHostnames:   bannedHostnames.Copy().Delete(pr.OriginalHostname),
				ParentKey:         ir,
				ParentSection:     pr.SectionName,
				WaypointError:     waypointError,
			}
			parentRefs = append(parentRefs, rpi)
		}
		for _, gw := range currentParents {
			// Append all matches. Note we may be adding mismatch section or ports; this is handled later
			appendParent(gw, pk)
		}
	}
	// Ensure stable order
	slices.SortBy(parentRefs, func(a routeParentReference) string {
		return parentRefString(a.OriginalReference, localNamespace)
	})
	return parentRefs
}

func convertTCPRoute(ctx RouteContext, r k8salpha.TCPRouteRule, obj *k8salpha.TCPRoute, enforceRefGrant bool) (*istio.TCPRoute, *ConfigError) {
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
	dest, backendErr, err := buildTCPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant, gvk.TCPRoute)
	if err != nil {
		return nil, err
	}
	return &istio.TCPRoute{
		Route: dest,
	}, backendErr
}

func convertTLSRoute(ctx RouteContext, r k8salpha.TLSRouteRule, obj *k8salpha.TLSRoute, enforceRefGrant bool) (*istio.TLSRoute, *ConfigError) {
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
	dest, backendErr, err := buildTCPDestination(ctx, r.BackendRefs, obj.Namespace, enforceRefGrant, gvk.TLSRoute)
	if err != nil {
		return nil, err
	}
	return &istio.TLSRoute{
		Match: buildTLSMatch(obj.Spec.Hostnames),
		Route: dest,
	}, backendErr
}

func buildTCPDestination(
	ctx RouteContext,
	forwardTo []k8s.BackendRef,
	ns string,
	enforceRefGrant bool,
	k config.GroupVersionKind,
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
		dst, _, err := buildDestination(ctx, fwd, ns, enforceRefGrant, k)
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

func grpcWeightSum(forwardTo []k8s.GRPCBackendRef) int {
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
	ctx RouteContext,
	forwardTo []k8s.HTTPBackendRef,
	ns string,
	enforceRefGrant bool,
) ([]*istio.HTTPRouteDestination, *inferencePoolConfig, *ConfigError, *ConfigError) {
	if forwardTo == nil {
		return nil, nil, nil, nil
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
	var ipCfg *inferencePoolConfig
	res := []*istio.HTTPRouteDestination{}
	for i, fwd := range action {
		dst, ipconfig, err := buildDestination(ctx, fwd.BackendRef, ns, enforceRefGrant, gvk.HTTPRoute)
		ipCfg = ipconfig
		if err != nil {
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, ipCfg, nil, err
			}
		}
		rd := &istio.HTTPRouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		}
		for _, filter := range fwd.Filters {
			switch filter.Type {
			case k8s.HTTPRouteFilterRequestHeaderModifier:
				h := createHeadersFilter(filter.RequestHeaderModifier)
				if h == nil {
					continue
				}
				if rd.Headers == nil {
					rd.Headers = &istio.Headers{}
				}
				rd.Headers.Request = h
			case k8s.HTTPRouteFilterResponseHeaderModifier:
				h := createHeadersFilter(filter.ResponseHeaderModifier)
				if h == nil {
					continue
				}
				if rd.Headers == nil {
					rd.Headers = &istio.Headers{}
				}
				rd.Headers.Response = h
			default:
				return nil, ipCfg, nil, &ConfigError{Reason: InvalidFilter, Message: fmt.Sprintf("unsupported filter type %q", filter.Type)}
			}
		}
		res = append(res, rd)
	}
	return res, ipCfg, invalidBackendErr, nil
}

func buildGRPCDestination(
	ctx RouteContext,
	forwardTo []k8s.GRPCBackendRef,
	ns string,
	enforceRefGrant bool,
) ([]*istio.HTTPRouteDestination, *ConfigError, *ConfigError) {
	if forwardTo == nil {
		return nil, nil, nil
	}
	weights := []int{}
	action := []k8s.GRPCBackendRef{}
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
		dst, _, err := buildDestination(ctx, fwd.BackendRef, ns, enforceRefGrant, gvk.GRPCRoute)
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
			case k8s.GRPCRouteFilterRequestHeaderModifier:
				h := createHeadersFilter(filter.RequestHeaderModifier)
				if h == nil {
					continue
				}
				if rd.Headers == nil {
					rd.Headers = &istio.Headers{}
				}
				rd.Headers.Request = h
			case k8s.GRPCRouteFilterResponseHeaderModifier:
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

type inferencePoolConfig struct {
	enableExtProc      bool
	endpointPickerDst  string
	endpointPickerPort string
}

func buildDestination(ctx RouteContext, to k8s.BackendRef, ns string,
	enforceRefGrant bool, k config.GroupVersionKind,
) (*istio.Destination, *inferencePoolConfig, *ConfigError) {
	// check if the reference is allowed
	if enforceRefGrant {
		if toNs := to.Namespace; toNs != nil && string(*toNs) != ns {
			if !ctx.Grants.BackendAllowed(ctx.Krt, k, to.Name, *toNs, ns) {
				return &istio.Destination{}, nil, &ConfigError{
					Reason:  InvalidDestinationPermit,
					Message: fmt.Sprintf("backendRef %v/%v not accessible to a %s in namespace %q (missing a ReferenceGrant?)", to.Name, *toNs, k.Kind, ns),
				}
			}
		}
	}

	namespace := ptr.OrDefault((*string)(to.Namespace), ns)
	var invalidBackendErr *ConfigError
	var hostname string
	ref := normalizeReference(to.Group, to.Kind, gvk.Service)
	switch ref {
	case gvk.Service:
		if strings.Contains(string(to.Name), ".") {
			return nil, nil, &ConfigError{Reason: InvalidDestination, Message: "service name invalid; the name of the Service must be used, not the hostname."}
		}
		hostname = fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, ctx.DomainSuffix)
		key := namespace + "/" + string(to.Name)
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))
		if svc == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
	case config.GroupVersionKind{Group: gvk.ServiceEntry.Group, Kind: "Hostname"}:
		if to.Namespace != nil {
			return nil, nil, &ConfigError{Reason: InvalidDestination, Message: "namespace may not be set with Hostname type"}
		}
		hostname = string(to.Name)
		if ctx.LookupHostname(hostname, namespace) == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
	case config.GroupVersionKind{Group: features.MCSAPIGroup, Kind: "ServiceImport"}:
		hostname = fmt.Sprintf("%s.%s.svc.clusterset.local", to.Name, namespace)
		if !features.EnableMCSHost {
			// They asked for ServiceImport, but actually don't have full support enabled...
			// No problem, we can just treat it as Service, which is already cross-cluster in this mode anyways
			hostname = fmt.Sprintf("%s.%s.svc.%s", to.Name, namespace, ctx.DomainSuffix)
		}
		// TODO: currently we are always looking for Service. We should be looking for ServiceImport when features.EnableMCSHost
		key := namespace + "/" + string(to.Name)
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))
		if svc == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
		}
	case gvk.InferencePool:
		if !features.SupportGatewayAPIInferenceExtension {
			return nil, nil, &ConfigError{
				Reason:  InvalidDestinationKind,
				Message: "InferencePool is not enabled. To enable, set SUPPORT_GATEWAY_API_INFERENCE_EXTENSION to true in istiod",
			}
		}
		if strings.Contains(string(to.Name), ".") {
			return nil, nil, &ConfigError{
				Reason:  InvalidDestination,
				Message: "InferencePool.Name invalid; the name of the InferencePool must be used, not the hostname.",
			}
		}
		infPool := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.InferencePools, krt.FilterKey(namespace+"/"+string(to.Name))))
		if infPool == nil {
			// Inference pool doesn't exist
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", to.Name)}
			return &istio.Destination{}, nil, invalidBackendErr
		}
		inferencePoolServiceName, _ := InferencePoolServiceName(string(to.Name))
		hostname := fmt.Sprintf("%s.%s.svc.%s", inferencePoolServiceName, namespace, ctx.DomainSuffix)
		svc := ctx.LookupHostname(hostname, namespace)
		if svc == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestinationNotFound, Message: fmt.Sprintf("backend(%s) not found", hostname)}
			return &istio.Destination{}, nil, invalidBackendErr
		}
		if svc.Attributes.Labels == nil {
			invalidBackendErr = &ConfigError{Reason: InvalidDestination, Message: "InferencePool service invalid, extensionRef labels not found"}
			return &istio.Destination{}, nil, invalidBackendErr
		}

		ipCfg := &inferencePoolConfig{
			enableExtProc: true,
		}
		if dst, ok := svc.Attributes.Labels[InferencePoolExtensionRefSvc]; ok {
			ipCfg.endpointPickerDst = fmt.Sprintf("%s.%s.svc.%s", dst, infPool.Namespace, ctx.DomainSuffix)
		}
		if p, ok := svc.Attributes.Labels[InferencePoolExtensionRefPort]; ok {
			ipCfg.endpointPickerPort = p
		}
		if ipCfg.endpointPickerDst == "" || ipCfg.endpointPickerPort == "" {
			invalidBackendErr = &ConfigError{Reason: InvalidDestination, Message: "InferencePool service invalid, extensionRef labels not found"}
		}
		return &istio.Destination{
			Host: hostname,
			// Port: &istio.PortSelector{Number: uint32(*to.Port)},
		}, ipCfg, invalidBackendErr
	default:
		return &istio.Destination{}, nil, &ConfigError{
			Reason:  InvalidDestinationKind,
			Message: fmt.Sprintf("referencing unsupported backendRef: group %q kind %q", ptr.OrEmpty(to.Group), ptr.OrEmpty(to.Kind)),
		}
	}
	// All types currently require a Port, so we do this for everything; consider making this per-type if we have future types
	// that do not require port.
	if to.Port == nil {
		// "Port is required when the referent is a Kubernetes Service."
		return nil, nil, &ConfigError{Reason: InvalidDestination, Message: "port is required in backendRef"}
	}
	return &istio.Destination{
		Host: hostname,
		Port: &istio.PortSelector{Number: uint32(*to.Port)},
	}, nil, invalidBackendErr
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

func createMirrorFilter(ctx RouteContext, filter *k8s.HTTPRequestMirrorFilter, ns string,
	enforceRefGrant bool, k config.GroupVersionKind,
) (*istio.HTTPMirrorPolicy, *ConfigError) {
	if filter == nil {
		return nil, nil
	}
	var weightOne int32 = 1
	dst, _, err := buildDestination(ctx, k8s.BackendRef{
		BackendObjectReference: filter.BackendRef,
		Weight:                 &weightOne,
	}, ns, enforceRefGrant, k)
	if err != nil {
		return nil, err
	}
	var percent *istio.Percent
	if f := filter.Fraction; f != nil {
		percent = &istio.Percent{Value: (100 * float64(f.Numerator)) / float64(ptr.OrDefault(f.Denominator, int32(100)))}
	} else if p := filter.Percent; p != nil {
		percent = &istio.Percent{Value: float64(*p)}
	}
	return &istio.HTTPMirrorPolicy{Destination: dst, Percentage: percent}, nil
}

func createRewriteFilter(filter *k8s.HTTPURLRewriteFilter) *istio.HTTPRewrite {
	if filter == nil {
		return nil
	}
	rewrite := &istio.HTTPRewrite{}
	if filter.Path != nil {
		switch filter.Path.Type {
		case k8s.PrefixMatchHTTPPathModifier:
			rewrite.Uri = strings.TrimSuffix(*filter.Path.ReplacePrefixMatch, "/")
			if rewrite.Uri == "" {
				// `/` means removing the prefix
				rewrite.Uri = "/"
			}
		case k8s.FullPathHTTPPathModifier:
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

func createCorsFilter(filter *k8s.HTTPCORSFilter) *istio.CorsPolicy {
	if filter == nil {
		return nil
	}
	res := &istio.CorsPolicy{}
	for _, r := range filter.AllowOrigins {
		rs := string(r)
		if len(rs) == 0 {
			continue // Not valid anyways, but double check
		}

		// TODO: support wildcards (https://github.com/kubernetes-sigs/gateway-api/issues/3648)
		res.AllowOrigins = append(res.AllowOrigins, &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: string(r)},
		})
	}
	if filter.AllowCredentials {
		res.AllowCredentials = wrappers.Bool(true)
	}
	for _, r := range filter.AllowMethods {
		res.AllowMethods = append(res.AllowMethods, string(r))
	}
	for _, r := range filter.AllowHeaders {
		res.AllowHeaders = append(res.AllowHeaders, string(r))
	}
	for _, r := range filter.ExposeHeaders {
		res.ExposeHeaders = append(res.ExposeHeaders, string(r))
	}
	if filter.MaxAge > 0 {
		res.MaxAge = durationpb.New(time.Duration(filter.MaxAge) * time.Second)
	}

	return res
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
		case k8s.FullPathHTTPPathModifier:
			resp.Uri = *filter.Path.ReplaceFullPath
		case k8s.PrefixMatchHTTPPathModifier:
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
		tp := k8s.QueryParamMatchExact
		if qp.Type != nil {
			tp = *qp.Type
		}
		switch tp {
		case k8s.QueryParamMatchExact:
			res[string(qp.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: qp.Value},
			}
		case k8s.QueryParamMatchRegularExpression:
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

func createGRPCHeadersMatch(match k8s.GRPCRouteMatch) (map[string]*istio.StringMatch, *ConfigError) {
	res := map[string]*istio.StringMatch{}
	for _, header := range match.Headers {
		tp := k8s.GRPCHeaderMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case k8s.GRPCHeaderMatchExact:
			res[string(header.Name)] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: header.Value},
			}
		case k8s.GRPCHeaderMatchRegularExpression:
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
		// "When specified, a trailing `/` is ignored."
		if dest != "/" {
			dest = strings.TrimSuffix(dest, "/")
		}
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

func createGRPCURIMatch(match k8s.GRPCRouteMatch) (*istio.StringMatch, *ConfigError) {
	m := match.Method
	if m == nil {
		return nil, nil
	}
	tp := k8s.GRPCMethodMatchExact
	if m.Type != nil {
		tp = *m.Type
	}
	if m.Method == nil && m.Service == nil {
		// Should never happen, invalid per spec
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: "gRPC match must have method or service defined"}
	}
	// gRPC format is /<Service>/<Method>. Since we don't natively understand this, convert to various string matches
	switch tp {
	case k8s.GRPCMethodMatchExact:
		if m.Method == nil {
			return &istio.StringMatch{
				MatchType: &istio.StringMatch_Prefix{Prefix: fmt.Sprintf("/%s/", *m.Service)},
			}, nil
		}
		if m.Service == nil {
			return &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: fmt.Sprintf("/[^/]+/%s", *m.Method)},
			}, nil
		}
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: fmt.Sprintf("/%s/%s", *m.Service, *m.Method)},
		}, nil
	case k8s.GRPCMethodMatchRegularExpression:
		if m.Method == nil {
			return &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: fmt.Sprintf("/%s/.+", *m.Service)},
			}, nil
		}
		if m.Service == nil {
			return &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: fmt.Sprintf("/[^/]+/%s", *m.Method)},
			}, nil
		}
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Regex{Regex: fmt.Sprintf("/%s/%s", *m.Service, *m.Method)},
		}, nil
	default:
		// Should never happen, unless a new field is added
		return nil, &ConfigError{Reason: InvalidConfiguration, Message: fmt.Sprintf("unknown type: %q is not supported Path match type", tp)}
	}
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

func (p parentKey) String() string {
	return p.Kind.String() + "/" + p.Namespace + "/" + p.Name
}

type parentReference struct {
	parentKey

	SectionName k8s.SectionName
	Port        k8s.PortNumber
}

func (p parentReference) String() string {
	return p.parentKey.String() + "/" + string(p.SectionName) + "/" + fmt.Sprint(p.Port)
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

// parentInfo holds info about a "parent" - something that can be referenced as a ParentRef in the API.
// Today, this is just Gateway and Mesh.
type parentInfo struct {
	// InternalName refers to the internal name we can reference it by. For example, "mesh" or "my-ns/my-gateway"
	InternalName string
	// AllowedKinds indicates which kinds can be admitted by this parent
	AllowedKinds []k8s.RouteGroupKind
	// Hostnames is the hostnames that must be match to reference to the parent. For gateway this is listener hostname
	// Format is ns/hostname or just hostname, which is equivalent to */hostname
	Hostnames []string
	// OriginalHostname is the unprocessed form of Hostnames; how it appeared in users' config
	OriginalHostname string

	SectionName k8s.SectionName
	Port        k8s.PortNumber
	Protocol    k8s.ProtocolType
}

// routeParentReference holds information about a route's parent reference
type routeParentReference struct {
	// InternalName refers to the internal name of the parent we can reference it by. For example, "mesh" or "my-ns/my-gateway"
	InternalName string
	// InternalKind is the Group/Kind of the parent
	InternalKind config.GroupVersionKind
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// OriginalReference contains the original reference
	OriginalReference k8s.ParentReference
	// Hostname is the hostname match of the parent, if any
	Hostname        string
	BannedHostnames sets.Set[string]
	ParentKey       parentKey
	ParentSection   k8s.SectionName
	// WaypointError, if present, indicates why the reference does not have valid configuration for generating a Waypoint
	WaypointError *WaypointError
}

func (r routeParentReference) IsMesh() bool {
	return r.InternalName == "mesh"
}

func (r routeParentReference) hostnameAllowedByIsolation(rawRouteHost string) bool {
	routeHost := host.Name(rawRouteHost)
	ourListener := host.Name(r.Hostname)
	if len(ourListener) > 0 && !ourListener.IsWildCarded() {
		// Short circuit: this logic only applies to wildcards
		// Not required for correctness, just an optimization
		return true
	}
	if len(ourListener) > 0 && !routeHost.Matches(ourListener) {
		return false
	}
	for checkListener := range r.BannedHostnames {
		// We have 3 hostnames here:
		// * routeHost, the hostname in the route entry
		// * ourListener, the hostname of the listener the route is bound to
		// * checkListener, the hostname of the other listener we are comparing to
		// We want to return false if checkListener would match the routeHost and it would be a more exact match
		if len(ourListener) > len(checkListener) {
			// If our hostname is longer, it must be more exact than the check
			continue
		}
		// Ours is shorter. If it matches the checkListener, then it should ONLY match that one
		// Note protocol, port, etc are already considered when we construct bannedHostnames
		if routeHost.SubsetOf(host.Name(checkListener)) {
			return false
		}
	}
	return true
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

func getDefaultName(name string, kgw *k8s.GatewaySpec, disableNameSuffix bool) string {
	if disableNameSuffix {
		return name
	}
	return fmt.Sprintf("%v-%v", name, kgw.GatewayClassName)
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

func unexpectedEastWestWaypointListener(l k8s.Listener) bool {
	if l.Port != 15008 {
		return true
	}
	if l.Protocol != k8s.ProtocolType(protocol.HBONE) {
		return true
	}
	if l.TLS == nil || *l.TLS.Mode != k8s.TLSModeTerminate {
		return true
	}
	// TODO: Should we check that there aren't more things set
	return false
}

func getListenerNames(spec *k8s.GatewaySpec) sets.Set[k8s.SectionName] {
	res := sets.New[k8s.SectionName]()
	for _, l := range spec.Listeners {
		res.Insert(l.Name)
	}
	return res
}

func reportGatewayStatus(
	r *GatewayContext,
	obj *k8sbeta.Gateway,
	gs *k8sbeta.GatewayStatus,
	classInfo classInfo,
	gatewayServices []string,
	servers []*istio.Server,
	listenerSetCount int,
	gatewayErr *ConfigError,
) {
	// TODO: we lose address if servers is empty due to an error
	internal, internalIP, external, pending, warnings, allUsable := r.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(k8s.GatewayConditionAccepted): {
			reason:  string(k8s.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(k8s.GatewayConditionProgrammed): {
			reason:  string(k8s.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if gatewayErr != nil {
		gatewayConditions[string(k8s.GatewayConditionAccepted)].error = gatewayErr
	}

	// Not defined in upstream API
	const AttachedListenerSets = "AttachedListenerSets"
	if obj.Spec.AllowedListeners != nil {
		gatewayConditions[AttachedListenerSets] = &condition{
			reason:  "ListenersAttached",
			message: "At least one ListenerSet is attached",
		}
		if !features.EnableAlphaGatewayAPI {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason: "Unsupported",
				Message: fmt.Sprintf("AllowedListeners is configured, but ListenerSets are not enabled (set %v=true)",
					features.EnableAlphaGatewayAPIName),
			}
		} else if listenerSetCount == 0 {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason:  "NoListenersAttached",
				Message: "AllowedListeners is configured, but no ListenerSets are attached",
			}
		}
	}

	setProgrammedCondition(gatewayConditions, internal, gatewayServices, warnings, allUsable)

	addressesToReport := external
	if len(addressesToReport) == 0 {
		wantAddressType := classInfo.addressType
		if override, ok := obj.Annotations[addressTypeOverride]; ok {
			wantAddressType = k8s.AddressType(override)
		}
		// There are no external addresses, so report the internal ones
		// This can be IP, Hostname, or both (indicated by empty wantAddressType)
		if wantAddressType != k8s.HostnameAddressType {
			addressesToReport = internalIP
		}
		if wantAddressType != k8s.IPAddressType {
			for _, hostport := range internal {
				svchost, _, _ := net.SplitHostPort(hostport)
				if !slices.Contains(pending, svchost) && !slices.Contains(addressesToReport, svchost) {
					addressesToReport = append(addressesToReport, svchost)
				}
			}
		}
	}
	// Do not report an address until we are ready. But once we are ready, never remove the address.
	if len(addressesToReport) > 0 {
		gs.Addresses = make([]k8s.GatewayStatusAddress, 0, len(addressesToReport))
		for _, addr := range addressesToReport {
			var addrType k8s.AddressType
			if _, err := netip.ParseAddr(addr); err == nil {
				addrType = k8s.IPAddressType
			} else {
				addrType = k8s.HostnameAddressType
			}
			gs.Addresses = append(gs.Addresses, k8s.GatewayStatusAddress{
				Value: addr,
				Type:  &addrType,
			})
		}
	}
	// Prune listeners that have been removed
	haveListeners := getListenerNames(&obj.Spec)
	listeners := make([]k8s.ListenerStatus, 0, len(gs.Listeners))
	for _, l := range gs.Listeners {
		if haveListeners.Contains(l.Name) {
			haveListeners.Delete(l.Name)
			listeners = append(listeners, l)
		}
	}
	gs.Listeners = listeners
	gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
}

func reportListenerSetStatus(
	r *GatewayContext,
	parentGwObj *k8sbeta.Gateway,
	obj *gatewayx.XListenerSet,
	gs *gatewayx.ListenerSetStatus,
	gatewayServices []string,
	servers []*istio.Server,
	gatewayErr *ConfigError,
) {
	internal, _, _, _, warnings, allUsable := r.ResolveGatewayInstances(parentGwObj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(k8s.GatewayConditionAccepted): {
			reason:  string(k8s.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(k8s.GatewayConditionProgrammed): {
			reason:  string(k8s.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if gatewayErr != nil {
		gatewayErr.Message = "Parent not accepted: " + gatewayErr.Message
		gatewayConditions[string(k8s.GatewayConditionAccepted)].error = gatewayErr
	}

	setProgrammedCondition(gatewayConditions, internal, gatewayServices, warnings, allUsable)

	gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
}

func setProgrammedCondition(gatewayConditions map[string]*condition, internal []string, gatewayServices []string, warnings []string, allUsable bool) {
	if len(internal) > 0 {
		msg := fmt.Sprintf("Resource programmed, assigned to service(s) %s", humanReadableJoin(internal))
		gatewayConditions[string(k8s.GatewayConditionProgrammed)].message = msg
	}

	if len(gatewayServices) == 0 {
		gatewayConditions[string(k8s.GatewayConditionProgrammed)].error = &ConfigError{
			Reason:  InvalidAddress,
			Message: "Failed to assign to any requested addresses",
		}
	} else if len(warnings) > 0 {
		var msg string
		var reason string
		if len(internal) != 0 {
			msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
				humanReadableJoin(internal), strings.Join(warnings, "; "))
		} else {
			msg = fmt.Sprintf("Failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
		}
		if allUsable {
			reason = string(k8s.GatewayReasonAddressNotAssigned)
		} else {
			reason = string(k8s.GatewayReasonAddressNotUsable)
		}
		gatewayConditions[string(k8s.GatewayConditionProgrammed)].error = &ConfigError{
			// TODO: this only checks Service ready, we should also check Deployment ready?
			Reason:  reason,
			Message: msg,
		}
	}
}

// reportUnmanagedGatewayStatus reports a status message for an unmanaged gateway.
// For these gateways, we don't deploy them. However, all gateways ought to have a status message, even if its basically
// just to say something read it
func reportUnmanagedGatewayStatus(
	status *k8sbeta.GatewayStatus,
	obj *k8sbeta.Gateway,
) {
	gatewayConditions := map[string]*condition{
		string(k8s.GatewayConditionAccepted): {
			reason:  string(k8s.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(k8s.GatewayConditionProgrammed): {
			reason: string(k8s.GatewayReasonProgrammed),
			// Set to true anyway since this is basically declaring it as valid
			message: "This Gateway is remote; Istio will not program it",
		},
	}

	status.Addresses = slices.Map(obj.Spec.Addresses, func(e k8s.GatewaySpecAddress) k8s.GatewayStatusAddress {
		return k8s.GatewayStatusAddress(e)
	})
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

// reportUnsupportedListenerSet reports a status message for a ListenerSet that is not supported
func reportUnsupportedListenerSet(class string, status *gatewayx.ListenerSetStatus, obj *gatewayx.XListenerSet) {
	gatewayConditions := map[string]*condition{
		string(k8s.GatewayConditionAccepted): {
			reason: string(k8s.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
		string(k8s.GatewayConditionProgrammed): {
			reason: string(k8s.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

// reportNotAllowedListenerSet reports a status message for a ListenerSet that is not allowed to be selected
func reportNotAllowedListenerSet(status *gatewayx.ListenerSetStatus, obj *gatewayx.XListenerSet) {
	gatewayConditions := map[string]*condition{
		string(k8s.GatewayConditionAccepted): {
			reason: string(k8s.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
		string(k8s.GatewayConditionProgrammed): {
			reason: string(k8s.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
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
//
// If manual deployments are disabled, IsManaged() always returns true.
func IsManaged(gw *k8s.GatewaySpec) bool {
	if !features.EnableGatewayAPIManualDeployment {
		return true
	}
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

func extractGatewayServices(domainSuffix string, kgw *k8sbeta.Gateway, info classInfo) ([]string, *ConfigError) {
	if IsManaged(&kgw.Spec) {
		name := model.GetOrDefault(kgw.Annotations[annotation.GatewayNameOverride.Name], getDefaultName(kgw.Name, &kgw.Spec, info.disableNameSuffix))
		return []string{fmt.Sprintf("%s.%s.svc.%v", name, kgw.Namespace, domainSuffix)}, nil
	}
	gatewayServices := []string{}
	skippedAddresses := []string{}
	for _, addr := range kgw.Spec.Addresses {
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
			fqdn = fmt.Sprintf("%s.%s.svc.%s", fqdn, kgw.Namespace, domainSuffix)
		}
		gatewayServices = append(gatewayServices, fqdn)
	}
	if len(skippedAddresses) > 0 {
		// Give error but return services, this is a soft failure
		return gatewayServices, &ConfigError{
			Reason:  InvalidAddress,
			Message: fmt.Sprintf("only Hostname is supported, ignoring %v", skippedAddresses),
		}
	}
	if _, f := kgw.Annotations[annotation.NetworkingServiceType.Name]; f {
		// Give error but return services, this is a soft failure
		// Remove entirely in 1.20
		return gatewayServices, &ConfigError{
			Reason:  DeprecateFieldUsage,
			Message: fmt.Sprintf("annotation %v is deprecated, use Spec.Infrastructure.Routeability", annotation.NetworkingServiceType.Name),
		}
	}
	return gatewayServices, nil
}

func buildListener(
	ctx krt.HandlerContext,
	secrets krt.Collection[*corev1.Secret],
	grants ReferenceGrants,
	namespaces krt.Collection[*corev1.Namespace],
	obj controllers.Object,
	status []k8s.ListenerStatus,
	l k8s.Listener,
	listenerIndex int,
	controllerName k8s.GatewayController,
	portErr error,
) (*istio.Server, []k8s.ListenerStatus, bool) {
	listenerConditions := map[string]*condition{
		string(k8s.ListenerConditionAccepted): {
			reason:  string(k8s.ListenerReasonAccepted),
			message: "No errors found",
		},
		string(k8s.ListenerConditionProgrammed): {
			reason:  string(k8s.ListenerReasonProgrammed),
			message: "No errors found",
		},
		string(k8s.ListenerConditionConflicted): {
			reason:  string(k8s.ListenerReasonNoConflicts),
			message: "No errors found",
			status:  kstatus.StatusFalse,
		},
		string(k8s.ListenerConditionResolvedRefs): {
			reason:  string(k8s.ListenerReasonResolvedRefs),
			message: "No errors found",
		},
	}

	ok := true
	tls, err := buildTLS(ctx, secrets, grants, l.TLS, obj, kube.IsAutoPassthrough(obj.GetLabels(), l))
	if err != nil {
		listenerConditions[string(k8s.ListenerConditionResolvedRefs)].error = err
		listenerConditions[string(k8s.GatewayConditionProgrammed)].error = &ConfigError{
			Reason:  string(k8s.GatewayReasonInvalid),
			Message: "Bad TLS configuration",
		}
		ok = false
	}
	hostnames := buildHostnameMatch(ctx, obj.GetNamespace(), namespaces, l)
	if portErr != nil {
		listenerConditions[string(k8s.ListenerConditionAccepted)].error = &ConfigError{
			Reason:  string(k8s.ListenerReasonUnsupportedProtocol),
			Message: portErr.Error(),
		}
		ok = false
	}
	protocol, perr := listenerProtocolToIstio(controllerName, l.Protocol)
	if perr != nil {
		listenerConditions[string(k8s.ListenerConditionAccepted)].error = &ConfigError{
			Reason:  string(k8s.ListenerReasonUnsupportedProtocol),
			Message: perr.Error(),
		}
		ok = false
	}
	if controllerName == constants.ManagedGatewayMeshController {
		if unexpectedWaypointListener(l) {
			listenerConditions[string(k8s.ListenerConditionAccepted)].error = &ConfigError{
				Reason:  string(k8s.ListenerReasonUnsupportedProtocol),
				Message: `Expected a single listener on port 15008 with protocol "HBONE"`,
			}
		}
	}

	if controllerName == constants.ManagedGatewayEastWestController {
		if unexpectedEastWestWaypointListener(l) {
			listenerConditions[string(k8s.ListenerConditionAccepted)].error = &ConfigError{
				Reason:  string(k8s.ListenerReasonUnsupportedProtocol),
				Message: `Expected a single listener on port 15008 with protocol "HBONE" and TLS.Mode == Terminate`,
			}
		}
	}
	server := &istio.Server{
		Port: &istio.Port{
			// Name is required. We only have one server per Gateway, so we can just name them all the same
			Name:     "default",
			Number:   uint32(l.Port),
			Protocol: protocol,
		},
		Hosts: hostnames,
		Tls:   tls,
	}

	updatedStatus := reportListenerCondition(listenerIndex, l, obj, status, listenerConditions)
	return server, updatedStatus, ok
}

var supportedProtocols = sets.New(
	k8s.HTTPProtocolType,
	k8s.HTTPSProtocolType,
	k8s.TLSProtocolType,
	k8s.TCPProtocolType,
	k8s.ProtocolType(protocol.HBONE))

func listenerProtocolToIstio(name k8s.GatewayController, p k8s.ProtocolType) (string, error) {
	switch p {
	// Standard protocol types
	case k8s.HTTPProtocolType:
		return string(p), nil
	case k8s.HTTPSProtocolType:
		return string(p), nil
	case k8s.TLSProtocolType, k8s.TCPProtocolType:
		if !features.EnableAlphaGatewayAPI {
			return "", fmt.Errorf("protocol %q is supported, but only when %v=true is configured", p, features.EnableAlphaGatewayAPIName)
		}
		return string(p), nil
	// Our own custom types
	case k8s.ProtocolType(protocol.HBONE):
		if name != constants.ManagedGatewayMeshController && name != constants.ManagedGatewayEastWestController {
			return "", fmt.Errorf("protocol %q is only supported for waypoint proxies", p)
		}
		return string(p), nil
	}
	up := k8s.ProtocolType(strings.ToUpper(string(p)))
	if supportedProtocols.Contains(up) {
		return "", fmt.Errorf("protocol %q is unsupported. hint: %q (uppercase) may be supported", p, up)
	}
	// Note: the k8s.UDPProtocolType is explicitly left to hit this path
	return "", fmt.Errorf("protocol %q is unsupported", p)
}

func buildTLS(
	ctx krt.HandlerContext,
	secrets krt.Collection[*corev1.Secret],
	grants ReferenceGrants,
	tls *k8s.GatewayTLSConfig,
	gw controllers.Object,
	isAutoPassthrough bool,
) (*istio.ServerTLSSettings, *ConfigError) {
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
	namespace := gw.GetNamespace()
	switch mode {
	case k8s.TLSModeTerminate:
		out.Mode = istio.ServerTLSSettings_SIMPLE
		if tls.Options != nil {
			switch tls.Options[gatewayTLSTerminateModeKey] {
			case "MUTUAL":
				out.Mode = istio.ServerTLSSettings_MUTUAL
			case "OPTIONAL_MUTUAL":
				out.Mode = istio.ServerTLSSettings_OPTIONAL_MUTUAL
			case "ISTIO_SIMPLE":
				// Simple TLS but with builtin workload certificate.
				// equivalent to `credentialName: builtin://
				out.Mode = istio.ServerTLSSettings_SIMPLE
				out.CredentialName = creds.BuiltinGatewaySecretTypeURI
				return out, nil
			case "ISTIO_MUTUAL":
				out.Mode = istio.ServerTLSSettings_ISTIO_MUTUAL
				return out, nil
			}
		}
		if len(tls.CertificateRefs) > 2 {
			return out, &ConfigError{
				Reason:  InvalidTLS,
				Message: "TLS mode can only support up to 2 server certificates",
			}
		}
		credNames := make([]string, len(tls.CertificateRefs))
		validCertCount := 0
		var combinedErr *ConfigError
		for i, certRef := range tls.CertificateRefs {
			cred, err := buildSecretReference(ctx, certRef, gw, secrets)
			if err != nil {
				combinedErr = joinErrors(combinedErr, err)
				continue
			}
			credNs := ptr.OrDefault((*string)(certRef.Namespace), namespace)
			sameNamespace := credNs == namespace
			objectKind := schematypes.GvkFromObject(gw)
			if !sameNamespace && !grants.SecretAllowed(ctx, objectKind, creds.ToResourceName(cred), namespace) {
				combinedErr = joinErrors(combinedErr, &ConfigError{
					Reason: InvalidListenerRefNotPermitted,
					Message: fmt.Sprintf(
						"certificateRef %v/%v not accessible to a Gateway in namespace %q (missing a ReferenceGrant?)",
						certRef.Name, credNs, namespace,
					),
				})
				continue
			}
			credNames[i] = cred
			validCertCount++
		}
		if validCertCount == 0 {
			// If we have no valid certificates, return an error
			return out, combinedErr
		}
		if validCertCount == 1 {
			out.CredentialName = credNames[0]
		} else {
			out.CredentialNames = credNames
		}
	case k8s.TLSModePassthrough:
		out.Mode = istio.ServerTLSSettings_PASSTHROUGH
		if isAutoPassthrough {
			out.Mode = istio.ServerTLSSettings_AUTO_PASSTHROUGH
		}
	}
	return out, nil
}

func buildSecretReference(
	ctx krt.HandlerContext,
	ref k8s.SecretObjectReference,
	gw controllers.Object,
	secrets krt.Collection[*corev1.Secret],
) (string, *ConfigError) {
	if normalizeReference(ref.Group, ref.Kind, gvk.Secret) != gvk.Secret {
		return "", &ConfigError{Reason: InvalidTLS, Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", objectReferenceString(ref))}
	}

	secret := model.ConfigKey{
		Kind:      kind.Secret,
		Name:      string(ref.Name),
		Namespace: ptr.OrDefault((*string)(ref.Namespace), gw.GetNamespace()),
	}

	key := secret.Namespace + "/" + secret.Name
	scrt := ptr.Flatten(krt.FetchOne(ctx, secrets, krt.FilterKey(key)))
	if scrt == nil {
		return "", &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, secret %v not found", objectReferenceString(ref), key),
		}
	}
	certInfo, err := kubecreds.ExtractCertInfo(scrt)
	if err != nil {
		return "", &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, %v", objectReferenceString(ref), err),
		}
	}
	if _, err = tls.X509KeyPair(certInfo.Cert, certInfo.Key); err != nil {
		return "", &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, the certificate is malformed: %v", objectReferenceString(ref), err),
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

func parentRefString(ref k8s.ParentReference, objectNamespace string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d.%s",
		defaultString(ref.Group, gvk.KubernetesGateway.Group),
		defaultString(ref.Kind, gvk.KubernetesGateway.Kind),
		ref.Name,
		ptr.OrEmpty(ref.SectionName),
		ptr.OrEmpty(ref.Port),
		defaultString(ref.Namespace, objectNamespace))
}

// buildHostnameMatch generates a Gateway.spec.servers.hosts section from a listener
func buildHostnameMatch(ctx krt.HandlerContext, localNamespace string, namespaces krt.Collection[*corev1.Namespace], l k8s.Listener) []string {
	// We may allow all hostnames or a specific one
	hostname := "*"
	if l.Hostname != nil {
		hostname = string(*l.Hostname)
	}

	resp := []string{}
	for _, ns := range namespacesFromSelector(ctx, localNamespace, namespaces, l.AllowedRoutes) {
		// This check is necessary to prevent adding a hostname with an invalid empty namespace
		if len(ns) > 0 {
			resp = append(resp, fmt.Sprintf("%s/%s", ns, hostname))
		}
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
func namespacesFromSelector(ctx krt.HandlerContext, localNamespace string, namespaceCol krt.Collection[*corev1.Namespace], lr *k8s.AllowedRoutes) []string {
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
	namespaceObjects := krt.Fetch(ctx, namespaceCol)
	for _, ns := range namespaceObjects {
		if ls.Matches(toNamespaceSet(ns.Name, ns.Labels)) {
			namespaces = append(namespaces, ns.Name)
		}
	}
	// Ensure stable order
	sort.Strings(namespaces)
	return namespaces
}

// namespaceAcceptedByAllowListeners determines a list of allowed namespaces for a given AllowedListener
func namespaceAcceptedByAllowListeners(localNamespace string, parent *k8sbeta.Gateway, lookupNamespace func(string) *corev1.Namespace) bool {
	lr := parent.Spec.AllowedListeners
	// Default allows none
	if lr == nil || lr.Namespaces == nil {
		return false
	}
	n := *lr.Namespaces
	if n.From != nil {
		switch *n.From {
		case k8s.NamespacesFromAll:
			return true
		case k8s.NamespacesFromSame:
			return localNamespace == parent.Namespace
		case k8s.NamespacesFromNone:
			return false
		default:
			// Unknown?
			return false
		}
	}
	if lr.Namespaces.Selector == nil {
		// Should never happen, invalid config
		return false
	}
	ls, err := metav1.LabelSelectorAsSelector(lr.Namespaces.Selector)
	if err != nil {
		return false
	}
	localNamespaceObject := lookupNamespace(localNamespace)
	if localNamespaceObject == nil {
		// Couldn't find the namespace
		return false
	}
	return ls.Matches(toNamespaceSet(localNamespaceObject.Name, localNamespaceObject.Labels))
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

func GetCommonRouteInfo(spec any) ([]k8s.ParentReference, []k8s.Hostname, config.GroupVersionKind) {
	switch t := spec.(type) {
	case *k8salpha.TCPRoute:
		return t.Spec.ParentRefs, nil, gvk.TCPRoute
	case *k8salpha.TLSRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.TLSRoute
	case *k8sbeta.HTTPRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.HTTPRoute
	case *k8s.GRPCRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.GRPCRoute
	default:
		log.Fatalf("unknown type %T", t)
		return nil, nil, config.GroupVersionKind{}
	}
}

func GetCommonRouteStateParents(spec any) []k8s.RouteParentStatus {
	switch t := spec.(type) {
	case *k8salpha.TCPRoute:
		return t.Status.Parents
	case *k8salpha.TLSRoute:
		return t.Status.Parents
	case *k8sbeta.HTTPRoute:
		return t.Status.Parents
	case *k8s.GRPCRoute:
		return t.Status.Parents
	default:
		log.Fatalf("unknown type %T", t)
		return nil
	}
}

// normalizeReference takes a generic Group/Kind (the API uses a few variations) and converts to a known GroupVersionKind.
// Defaults for the group/kind are also passed.
func normalizeReference[G ~string, K ~string](group *G, kind *K, def config.GroupVersionKind) config.GroupVersionKind {
	k := def.Kind
	if kind != nil {
		k = string(*kind)
	}
	g := def.Group
	if group != nil {
		g = string(*group)
	}
	gk := config.GroupVersionKind{
		Group: g,
		Kind:  k,
	}
	s, f := collections.All.FindByGroupKind(gk)
	if f {
		return s.GroupVersionKind()
	}
	return gk
}

func defaultString[T ~string](s *T, def string) string {
	if s == nil {
		return def
	}
	return string(*s)
}

func toRouteKind(g config.GroupVersionKind) k8s.RouteGroupKind {
	return k8s.RouteGroupKind{Group: (*k8s.Group)(&g.Group), Kind: k8s.Kind(g.Kind)}
}

func routeGroupKindEqual(rgk1, rgk2 k8s.RouteGroupKind) bool {
	return rgk1.Kind == rgk2.Kind && getGroup(rgk1) == getGroup(rgk2)
}

func getGroup(rgk k8s.RouteGroupKind) k8s.Group {
	return ptr.OrDefault(rgk.Group, k8s.Group(gvk.KubernetesGateway.Group))
}

func GetStatus[I, IS any](spec I) IS {
	switch t := any(spec).(type) {
	case *k8salpha.TCPRoute:
		return any(t.Status).(IS)
	case *k8salpha.TLSRoute:
		return any(t.Status).(IS)
	case *k8sbeta.HTTPRoute:
		return any(t.Status).(IS)
	case *k8s.GRPCRoute:
		return any(t.Status).(IS)
	case *k8sbeta.Gateway:
		return any(t.Status).(IS)
	case *k8sbeta.GatewayClass:
		return any(t.Status).(IS)
	case *gatewayx.XBackendTrafficPolicy:
		return any(t.Status).(IS)
	case *gatewayalpha3.BackendTLSPolicy:
		return any(t.Status).(IS)
	case *gatewayx.XListenerSet:
		return any(t.Status).(IS)
	case *inferencev1alpha2.InferencePool:
		return any(t.Status).(IS)
	default:
		log.Fatalf("unknown type %T", t)
		return ptr.Empty[IS]()
	}
}
