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
	"iter"
	"strconv"
	"strings"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	istio "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway/kube"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type AncestorBackend struct {
	Gateway types.NamespacedName
	Backend TypedNamespacedName
}

func (a AncestorBackend) Equals(other AncestorBackend) bool {
	return a.Gateway == other.Gateway && a.Backend == other.Backend
}

func (a AncestorBackend) ResourceName() string {
	return a.Gateway.String() + "/" + a.Backend.String()
}

func HTTPRouteCollection(
	httpRoutes krt.Collection[*gatewayv1.HTTPRoute],
	inputs RouteContextInputs,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayv1.HTTPRoute, gatewayv1.HTTPRouteStatus] {
	routeCount := gatewayRouteAttachmentCountCollection(inputs, httpRoutes, gvk.HTTPRoute, opts)
	ancestorBackends := krt.NewManyCollection(httpRoutes, func(krtctx krt.HandlerContext, obj *gatewayv1.HTTPRoute) []AncestorBackend {
		return extractAncestorBackends(obj.Namespace, obj.Spec.ParentRefs, obj.Spec.Rules, func(r gatewayv1.HTTPRouteRule) []gatewayv1.HTTPBackendRef {
			return r.BackendRefs
		})
	}, opts.WithName("HTTPAncestors")...)
	status, baseVirtualServices := krt.NewStatusManyCollection(httpRoutes, func(krtctx krt.HandlerContext, obj *gatewayv1.HTTPRoute) (
		*gatewayv1.HTTPRouteStatus,
		[]RouteWithKey,
	) {
		ctx := inputs.WithCtx(krtctx)
		inferencePoolCfgPairs := []struct {
			name string
			cfg  *inferencePoolConfig
		}{}
		status := obj.Status.DeepCopy()
		route := obj.Spec
		parentStatus, parentRefs, meshResult, gwResult := computeRoute(ctx, obj, func(mesh bool, obj *gatewayv1.HTTPRoute) iter.Seq2[*istio.HTTPRoute, *ConfigError] {
			return func(yield func(*istio.HTTPRoute, *ConfigError) bool) {
				for n, r := range route.Rules {
					// split the rule to make sure each rule has up to one match
					matches := slices.Reference(r.Matches)
					if len(matches) == 0 {
						matches = append(matches, nil)
					}
					for _, m := range matches {
						if m != nil {
							r.Matches = []gatewayv1.HTTPRouteMatch{*m}
						}
						istioRoute, ipCfg, configErr := convertHTTPRoute(ctx, r, obj, n, !mesh)
						if istioRoute != nil && ipCfg != nil && ipCfg.enableExtProc {
							inferencePoolCfgPairs = append(inferencePoolCfgPairs, struct {
								name string
								cfg  *inferencePoolConfig
							}{name: istioRoute.Name, cfg: ipCfg})
						}
						if !yield(istioRoute, configErr) {
							return
						}
					}
				}
			}
		})

		// routeRuleToInferencePoolCfg stores inference pool configs discovered during route rule conversion,
		// keyed by the istio.HTTPRoute.Name.
		routeRuleToInferencePoolCfg := make(map[string]*inferencePoolConfig)
		for _, pair := range inferencePoolCfgPairs {
			routeRuleToInferencePoolCfg[pair.name] = pair.cfg
		}
		status.Parents = parentStatus

		count := 0
		virtualServices := []RouteWithKey{}
		for _, parent := range filteredReferences(parentRefs) {
			// for gateway routes, build one VS per gateway+host
			routeKey := parent.InternalName
			vsHosts := hostnameToStringList(route.Hostnames)
			routes := gwResult.routes
			if parent.IsMesh() {
				routes = meshResult.routes
				// for mesh routes, build one VS per namespace/port->host
				routeKey = obj.Namespace
				if parent.OriginalReference.Port != nil {
					routes = augmentPortMatch(routes, *parent.OriginalReference.Port)
					routeKey += "/" + strconv.Itoa(int(*parent.OriginalReference.Port))
				}
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{
						string(parent.OriginalReference.Name) +
							"." +
							string(ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace))) +
							".svc." + ctx.DomainSuffix,
					}
				}
			}
			if len(routes) == 0 {
				continue
			}
			// Create one VS per hostname with a single hostname.
			// This ensures we can treat each hostname independently, as the spec requires
			for _, h := range vsHosts {
				if !parent.hostnameAllowedByIsolation(h) {
					// TODO: standardize a status message for this upstream and report
					continue
				}
				name := obj.Name + "~" + strconv.Itoa(count) + "~" + constants.KubernetesGatewayName
				sortHTTPRoutes(routes)

				// Populate Extra field for inference pool configs
				extraData := make(map[string]any)
				currentRouteInferenceConfigs := make(map[string]kube.InferencePoolRouteRuleConfig)
				for _, httpRule := range routes { // These are []*istio.HTTPRoute
					if ipCfg, found := routeRuleToInferencePoolCfg[httpRule.Name]; found {
						currentRouteInferenceConfigs[httpRule.Name] = kube.InferencePoolRouteRuleConfig{
							FQDN:             ipCfg.endpointPickerDst,
							Port:             ipCfg.endpointPickerPort,
							FailureModeAllow: ipCfg.endpointPickerFailureMode == string(inferencev1.EndpointPickerFailOpen),
						}
					}
				}
				if len(currentRouteInferenceConfigs) > 0 {
					extraData[constants.ConfigExtraPerRouteRuleInferencePoolConfigs] = currentRouteInferenceConfigs
				}

				cfg := &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.DomainSuffix,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{h},
						Gateways: []string{parent.InternalName},
						Http:     routes,
					},
					Extra: extraData,
				}
				virtualServices = append(virtualServices, RouteWithKey{
					Config: cfg,
					Key:    routeKey + "/" + h,
				})
				count++
			}
		}
		return status, virtualServices
	}, opts.WithName("HTTPRoute")...)

	finalVirtualServices := mergeHTTPRoutes(baseVirtualServices, opts.WithName("HTTPRouteMerged")...)
	return RouteResult[*gatewayv1.HTTPRoute, gatewayv1.HTTPRouteStatus]{
		VirtualServices:  finalVirtualServices,
		RouteAttachments: routeCount,
		Status:           status,
		Ancestors:        ancestorBackends,
	}
}

func extractAncestorBackends[RT, BT any](ns string, prefs []gatewayv1.ParentReference, rules []RT, extract func(RT) []BT) []AncestorBackend {
	gateways := sets.Set[types.NamespacedName]{}
	for _, r := range prefs {
		ref := normalizeReference(r.Group, r.Kind, gvk.KubernetesGateway)
		if ref != gvk.KubernetesGateway {
			continue
		}
		gateways.Insert(types.NamespacedName{
			Namespace: defaultString(r.Namespace, ns),
			Name:      string(r.Name),
		})
	}
	backends := sets.Set[TypedNamespacedName]{}
	for _, r := range rules {
		for _, b := range extract(r) {
			ref, refNs, refName := GetBackendRef(b)
			k, ok := gvk.ToKind(ref)
			if !ok {
				continue
			}
			be := TypedNamespacedName{
				NamespacedName: types.NamespacedName{
					Namespace: defaultString(refNs, ns),
					Name:      string(refName),
				},
				Kind: k,
			}
			backends.Insert(be)
		}
	}
	gtw := slices.SortBy(gateways.UnsortedList(), types.NamespacedName.String)
	bes := slices.SortBy(backends.UnsortedList(), TypedNamespacedName.String)
	res := make([]AncestorBackend, 0, len(gtw)*len(bes))
	for _, gw := range gtw {
		for _, be := range bes {
			res = append(res, AncestorBackend{
				Gateway: gw,
				Backend: be,
			})
		}
	}
	return res
}

type conversionResult[O any] struct {
	error  *ConfigError
	routes []O
}

func GRPCRouteCollection(
	grpcRoutes krt.Collection[*gatewayv1.GRPCRoute],
	inputs RouteContextInputs,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayv1.GRPCRoute, gatewayv1.GRPCRouteStatus] {
	routeCount := gatewayRouteAttachmentCountCollection(inputs, grpcRoutes, gvk.GRPCRoute, opts)
	ancestorBackends := krt.NewManyCollection(grpcRoutes, func(krtctx krt.HandlerContext, obj *gatewayv1.GRPCRoute) []AncestorBackend {
		return extractAncestorBackends(obj.Namespace, obj.Spec.ParentRefs, obj.Spec.Rules, func(r gatewayv1.GRPCRouteRule) []gatewayv1.GRPCBackendRef {
			return r.BackendRefs
		})
	}, opts.WithName("GRPCAncestors")...)
	status, baseVirtualServices := krt.NewStatusManyCollection(grpcRoutes, func(krtctx krt.HandlerContext, obj *gatewayv1.GRPCRoute) (
		*gatewayv1.GRPCRouteStatus,
		[]RouteWithKey,
	) {
		ctx := inputs.WithCtx(krtctx)
		// routeRuleToInferencePoolCfg stores inference pool configs discovered during route rule conversion.
		// Note: GRPCRoute currently doesn't have inference pool logic, but adding for consistency.
		routeRuleToInferencePoolCfg := make(map[string]*inferencePoolConfig)
		status := obj.Status.DeepCopy()
		route := obj.Spec
		parentStatus, parentRefs, meshResult, gwResult := computeRoute(ctx, obj, func(mesh bool, obj *gatewayv1.GRPCRoute) iter.Seq2[*istio.HTTPRoute, *ConfigError] {
			return func(yield func(*istio.HTTPRoute, *ConfigError) bool) {
				for n, r := range route.Rules {
					// split the rule to make sure each rule has up to one match
					matches := slices.Reference(r.Matches)
					if len(matches) == 0 {
						matches = append(matches, nil)
					}
					for _, m := range matches {
						if m != nil {
							r.Matches = []gatewayv1.GRPCRouteMatch{*m}
						}
						// GRPCRoute conversion currently doesn't return ipCfg.
						istioRoute, configErr := convertGRPCRoute(ctx, r, obj, n, !mesh)
						// Placeholder if GRPCRoute ever supports inference pools via ipCfg:
						// if istioRoute != nil && ipCfg != nil && ipCfg.enableExtProc {
						// 	routeRuleToInferencePoolCfg[istioRoute.Name] = ipCfg
						// }
						if !yield(istioRoute, configErr) {
							return
						}
					}
				}
			}
		})
		status.Parents = parentStatus

		count := 0
		virtualServices := []RouteWithKey{}
		for _, parent := range filteredReferences(parentRefs) {
			// for gateway routes, build one VS per gateway+host
			routeKey := parent.InternalName
			vsHosts := hostnameToStringList(route.Hostnames)
			routes := gwResult.routes
			if parent.IsMesh() {
				routes = meshResult.routes
				// for mesh routes, build one VS per namespace/port->host
				routeKey = obj.Namespace
				if parent.OriginalReference.Port != nil {
					routes = augmentPortMatch(routes, *parent.OriginalReference.Port)
					routeKey += "/" + strconv.Itoa(int(*parent.OriginalReference.Port))
				}
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s",
						parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace)), ctx.DomainSuffix)}
				}
			}
			if len(routes) == 0 {
				continue
			}
			// Create one VS per hostname with a single hostname.
			// This ensures we can treat each hostname independently, as the spec requires
			for _, h := range vsHosts {
				if !parent.hostnameAllowedByIsolation(h) {
					// TODO: standardize a status message for this upstream and report
					continue
				}
				name := fmt.Sprintf("%s~%d~%s", obj.Name, count, constants.KubernetesGatewayName)
				sortHTTPRoutes(routes)

				// Populate Extra field for inference pool configs (if GRPCRoute supports them)
				extraData := make(map[string]any)
				currentRouteInferenceConfigs := make(map[string]kube.InferencePoolRouteRuleConfig)
				for _, httpRule := range routes {
					if ipCfg, found := routeRuleToInferencePoolCfg[httpRule.Name]; found { // This map will be empty for GRPCRoute for now
						currentRouteInferenceConfigs[httpRule.Name] = kube.InferencePoolRouteRuleConfig{
							FQDN:             ipCfg.endpointPickerDst,
							Port:             ipCfg.endpointPickerPort,
							FailureModeAllow: ipCfg.endpointPickerFailureMode == string(inferencev1.EndpointPickerFailOpen),
						}
					}
				}
				if len(currentRouteInferenceConfigs) > 0 {
					extraData[constants.ConfigExtraPerRouteRuleInferencePoolConfigs] = currentRouteInferenceConfigs
				}

				cfg := &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.DomainSuffix,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{h},
						Gateways: []string{parent.InternalName},
						Http:     routes,
					},
					Extra: extraData,
				}
				virtualServices = append(virtualServices, RouteWithKey{
					Config: cfg,
					Key:    routeKey + "/" + h,
				})
				count++
			}
		}
		return status, virtualServices
	}, opts.WithName("GRPCRoute")...)

	finalVirtualServices := mergeHTTPRoutes(baseVirtualServices, opts.WithName("GRPCRouteMerged")...)
	return RouteResult[*gatewayv1.GRPCRoute, gatewayv1.GRPCRouteStatus]{
		VirtualServices:  finalVirtualServices,
		RouteAttachments: routeCount,
		Status:           status,
		Ancestors:        ancestorBackends,
	}
}

func TCPRouteCollection(
	tcpRoutes krt.Collection[*gatewayalpha.TCPRoute],
	inputs RouteContextInputs,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayalpha.TCPRoute, gatewayalpha.TCPRouteStatus] {
	routeCount := gatewayRouteAttachmentCountCollection(inputs, tcpRoutes, gvk.TCPRoute, opts)
	ancestorBackends := krt.NewManyCollection(tcpRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TCPRoute) []AncestorBackend {
		return extractAncestorBackends(obj.Namespace, obj.Spec.ParentRefs, obj.Spec.Rules, func(r gatewayalpha.TCPRouteRule) []gatewayv1.BackendRef {
			return r.BackendRefs
		})
	}, opts.WithName("TCPAncestors")...)
	status, virtualServices := krt.NewStatusManyCollection(tcpRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TCPRoute) (
		*gatewayalpha.TCPRouteStatus,
		[]*config.Config,
	) {
		ctx := inputs.WithCtx(krtctx)
		status := obj.Status.DeepCopy()
		route := obj.Spec
		parentStatus, parentRefs, meshResult, gwResult := computeRoute(ctx, obj,
			func(mesh bool, obj *gatewayalpha.TCPRoute) iter.Seq2[*istio.TCPRoute, *ConfigError] {
				return func(yield func(*istio.TCPRoute, *ConfigError) bool) {
					for _, r := range route.Rules {
						if !yield(convertTCPRoute(ctx, r, obj, !mesh)) {
							return
						}
					}
				}
			})
		status.Parents = parentStatus

		vs := []*config.Config{}
		count := 0
		for _, parent := range filteredReferences(parentRefs) {
			routes := gwResult.routes
			vsHosts := []string{"*"}
			if parent.IsMesh() {
				routes = meshResult.routes
				if parent.OriginalReference.Port != nil {
					routes = augmentTCPPortMatch(routes, *parent.OriginalReference.Port)
				}
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, ctx.DomainSuffix)}
				}
			}
			for _, host := range vsHosts {
				name := fmt.Sprintf("%s~tcp~%d~%s", obj.Name, count, constants.KubernetesGatewayName)
				// Create one VS per hostname with a single hostname.
				// This ensures we can treat each hostname independently, as the spec requires
				vs = append(vs, &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.DomainSuffix,
					},
					Spec: &istio.VirtualService{
						// We can use wildcard here since each listener can have at most one route bound to it, so we have
						// a single VS per Gateway.
						Hosts:    []string{host},
						Gateways: []string{parent.InternalName},
						Tcp:      routes,
					},
				})
				count++
			}
		}
		return status, vs
	}, opts.WithName("TCPRoute")...)

	return RouteResult[*gatewayalpha.TCPRoute, gatewayalpha.TCPRouteStatus]{
		VirtualServices:  virtualServices,
		RouteAttachments: routeCount,
		Status:           status,
		Ancestors:        ancestorBackends,
	}
}

func TLSRouteCollection(
	tlsRoutes krt.Collection[*gatewayalpha.TLSRoute],
	inputs RouteContextInputs,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayalpha.TLSRoute, gatewayalpha.TLSRouteStatus] {
	routeCount := gatewayRouteAttachmentCountCollection(inputs, tlsRoutes, gvk.TLSRoute, opts)
	ancestorBackends := krt.NewManyCollection(tlsRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TLSRoute) []AncestorBackend {
		return extractAncestorBackends(obj.Namespace, obj.Spec.ParentRefs, obj.Spec.Rules, func(r gatewayalpha.TLSRouteRule) []gatewayv1.BackendRef {
			return r.BackendRefs
		})
	}, opts.WithName("TLSAncestors")...)
	status, virtualServices := krt.NewStatusManyCollection(tlsRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TLSRoute) (
		*gatewayalpha.TLSRouteStatus,
		[]*config.Config,
	) {
		ctx := inputs.WithCtx(krtctx)
		status := obj.Status.DeepCopy()
		route := obj.Spec
		parentStatus, parentRefs, meshResult, gwResult := computeRoute(ctx,
			obj, func(mesh bool, obj *gatewayalpha.TLSRoute) iter.Seq2[*istio.TLSRoute, *ConfigError] {
				return func(yield func(*istio.TLSRoute, *ConfigError) bool) {
					for _, r := range route.Rules {
						if !yield(convertTLSRoute(ctx, r, obj, !mesh)) {
							return
						}
					}
				}
			})
		status.Parents = parentStatus

		count := 0
		vs := []*config.Config{}
		for _, parent := range filteredReferences(parentRefs) {
			routes := gwResult.routes
			vsHosts := hostnameToStringList(route.Hostnames)
			if parent.IsMesh() {
				routes = meshResult.routes
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gatewayv1.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, ctx.DomainSuffix)}
				}
				routes = augmentTLSPortMatch(routes, parent.OriginalReference.Port, vsHosts)
			}
			for _, host := range vsHosts {
				name := fmt.Sprintf("%s~tls~%d~%s", obj.Name, count, constants.KubernetesGatewayName)
				filteredRoutes := routes
				if parent.IsMesh() {
					filteredRoutes = compatibleRoutesForHost(routes, host)
				}
				// Create one VS per hostname with a single hostname.
				// This ensures we can treat each hostname independently, as the spec requires
				vs = append(vs, &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.DomainSuffix,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{host},
						Gateways: []string{parent.InternalName},
						Tls:      filteredRoutes,
					},
				})
				count++
			}
		}
		return status, vs
	}, opts.WithName("TLSRoute")...)
	return RouteResult[*gatewayalpha.TLSRoute, gatewayalpha.TLSRouteStatus]{
		VirtualServices:  virtualServices,
		RouteAttachments: routeCount,
		Status:           status,
		Ancestors:        ancestorBackends,
	}
}

// computeRoute holds the common route building logic shared amongst all types
func computeRoute[T controllers.Object, O comparable](ctx RouteContext, obj T, translator func(
	mesh bool,
	obj T,
) iter.Seq2[O, *ConfigError],
) ([]gatewayv1.RouteParentStatus, []routeParentReference, conversionResult[O], conversionResult[O]) {
	parentRefs := extractParentReferenceInfo(ctx, ctx.RouteParents, obj)

	convertRules := func(mesh bool) conversionResult[O] {
		res := conversionResult[O]{}
		for vs, err := range translator(mesh, obj) {
			// This was a hard error
			if controllers.IsNil(vs) {
				res.error = err
				return conversionResult[O]{error: err}
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

	rpResults := slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
		res := RouteParentResult{
			OriginalReference: r.OriginalReference,
			DeniedReason:      r.DeniedReason,
			RouteError:        gwResult.error,
		}
		if r.IsMesh() {
			res.RouteError = meshResult.error
			res.WaypointError = r.WaypointError
		}
		return res
	})
	parents := createRouteStatus(rpResults, obj.GetNamespace(), obj.GetGeneration(), GetCommonRouteStateParents(obj))
	return parents, parentRefs, meshResult, gwResult
}

// RouteContext defines a common set of inputs to a route collection. This should be built once per route translation and
// not shared outside of that.
// The embedded RouteContextInputs is typically based into a collection, then translated to a RouteContext with RouteContextInputs.WithCtx().
type RouteContext struct {
	Krt krt.HandlerContext
	RouteContextInputs
}

func (r RouteContext) LookupHostname(hostname string, namespace string) *model.Service {
	if c := r.internalContext.Get(r.Krt).Load(); c != nil {
		return c.GetService(hostname, namespace)
	}
	return nil
}

type RouteContextInputs struct {
	Grants          ReferenceGrants
	RouteParents    RouteParents
	DomainSuffix    string
	Services        krt.Collection[*corev1.Service]
	Namespaces      krt.Collection[*corev1.Namespace]
	ServiceEntries  krt.Collection[*networkingclient.ServiceEntry]
	InferencePools  krt.Collection[*inferencev1.InferencePool]
	internalContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]]
}

func (i RouteContextInputs) WithCtx(krtctx krt.HandlerContext) RouteContext {
	return RouteContext{
		Krt:                krtctx,
		RouteContextInputs: i,
	}
}

type RouteWithKey struct {
	*config.Config
	Key string
}

func (r RouteWithKey) ResourceName() string {
	return config.NamespacedName(r.Config).String()
}

func (r RouteWithKey) Equals(o RouteWithKey) bool {
	return r.Config.Equals(o.Config)
}

// buildMeshAndGatewayRoutes contains common logic to build a set of routes with mesh and/or gateway semantics
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

// RouteResult holds the result of a route collection
type RouteResult[I controllers.Object, IStatus any] struct {
	// VirtualServices are the primary output that configures the internal routing logic
	VirtualServices krt.Collection[*config.Config]
	// RouteAttachments holds information about parent attachment to routes, used for computed the `attachedRoutes` count.
	RouteAttachments krt.Collection[RouteAttachment]
	// Status stores the status reports for the incoming object
	Status krt.StatusCollection[I, IStatus]
	// Ancestors stores information about Gateway --> Backend references
	Ancestors krt.Collection[AncestorBackend]
}

type RouteAttachment struct {
	From TypedResource
	// To is assumed to be a Gateway
	To           types.NamespacedName
	ListenerName string
}

func (r RouteAttachment) ResourceName() string {
	return r.From.Kind.String() + "/" + r.From.Name.String() + "/" + r.To.String() + "/" + r.ListenerName
}

func (r RouteAttachment) Equals(other RouteAttachment) bool {
	return r.From == other.From && r.To == other.To && r.ListenerName == other.ListenerName
}

// gatewayRouteAttachmentCountCollection holds the generic logic to determine the parents a route is attached to, used for
// computing the aggregated `attachedRoutes` status in Gateway.
func gatewayRouteAttachmentCountCollection[T controllers.Object](
	inputs RouteContextInputs,
	col krt.Collection[T],
	kind config.GroupVersionKind,
	opts krt.OptionsBuilder,
) krt.Collection[RouteAttachment] {
	return krt.NewManyCollection(col, func(krtctx krt.HandlerContext, obj T) []RouteAttachment {
		ctx := inputs.WithCtx(krtctx)
		from := TypedResource{
			Kind: kind,
			Name: config.NamespacedName(obj),
		}

		parentRefs := extractParentReferenceInfo(ctx, inputs.RouteParents, obj)
		return slices.MapFilter(filteredReferences(parentRefs), func(e routeParentReference) *RouteAttachment {
			if e.ParentKey.Kind != gvk.KubernetesGateway {
				return nil
			}
			return &RouteAttachment{
				From: from,
				To: types.NamespacedName{
					Name:      e.ParentKey.Name,
					Namespace: e.ParentKey.Namespace,
				},
				ListenerName: string(e.ParentSection),
			}
		})
	}, opts.WithName(kind.Kind+"/count")...)
}

// mergeHTTPRoutes merges HTTProutes by key. Gateway API has semantics for the ordering of `match` rules, that merges across resource.
// So we merge everything (by key) following that ordering logic, and sort into a linear list (how VirtualService semantics work).
func mergeHTTPRoutes(baseVirtualServices krt.Collection[RouteWithKey], opts ...krt.CollectionOption) krt.Collection[*config.Config] {
	idx := krt.NewIndex(baseVirtualServices, "key", func(o RouteWithKey) []string {
		return []string{o.Key}
	}).AsCollection(opts...)
	finalVirtualServices := krt.NewCollection(idx, func(ctx krt.HandlerContext, object krt.IndexObject[string, RouteWithKey]) **config.Config {
		configs := object.Objects
		if len(configs) == 1 {
			base := configs[0].Config
			nm := base.Meta.DeepCopy()
			// When dealing with a merge, we MUST take into account the merge key into the name.
			// Otherwise, we end up with broken state, where two inputs map to the same output which is not allowed by krt.
			// Because a lot of code assumes the object key is 'namespace/name', and the key always has slashes, we also translate the /
			nm.Name = strings.ReplaceAll(object.Key, "/", "~")
			return ptr.Of(&config.Config{
				Meta:   nm,
				Spec:   base.Spec,
				Status: base.Status,
				Extra:  base.Extra,
			})
		}
		sortRoutesByCreationTime(configs)
		base := configs[0].DeepCopy()
		baseVS := base.Spec.(*istio.VirtualService)
		// Deep copy the InferencePool configs map to avoid race conditions
		// The default DeepCopy() only does shallow copy of Extra field
		if base.Extra != nil {
			if ipConfigs, ok := base.Extra[constants.ConfigExtraPerRouteRuleInferencePoolConfigs].(map[string]kube.InferencePoolRouteRuleConfig); ok {
				// Create a new map to avoid modifying the shared underlying map
				newIPConfigs := make(map[string]kube.InferencePoolRouteRuleConfig, len(ipConfigs))
				for k, v := range ipConfigs {
					newIPConfigs[k] = v
				}
				base.Extra[constants.ConfigExtraPerRouteRuleInferencePoolConfigs] = newIPConfigs
			}
		}
		for i, config := range configs[1:] {
			thisVS := config.Spec.(*istio.VirtualService)
			baseVS.Http = append(baseVS.Http, thisVS.Http...)
			// append parents
			base.Annotations[constants.InternalParentNames] = fmt.Sprintf("%s,%s",
				base.Annotations[constants.InternalParentNames], config.Annotations[constants.InternalParentNames])
			// Merge Extra field (especially for InferencePool configs)
			if base.Extra == nil && config.Extra != nil {
				base.Extra = make(map[string]any)
			}
			if config.Extra != nil {
				for k, v := range config.Extra {
					// For non-InferencePool configs, keep the first value for stability
					if k != constants.ConfigExtraPerRouteRuleInferencePoolConfigs {
						if _, exists := base.Extra[k]; !exists {
							base.Extra[k] = v
						}
						continue
					}
					// For InferencePool configs, merge the maps
					baseMap, baseOk := base.Extra[k].(map[string]kube.InferencePoolRouteRuleConfig)
					configMap, configOk := v.(map[string]kube.InferencePoolRouteRuleConfig)
					if baseOk && configOk {
						log.Debugf("Merging InferencePool configs: adding %d route configs from VirtualService %d to base (namespace=%s)",
							len(configMap), i+1, config.Namespace)
						// Route names are composed of the HTTPRoute/VirtualService namespaced name so they can't possibly conflict
						for routeName, routeConfig := range configMap {
							baseMap[routeName] = routeConfig
						}
					} else if configOk {
						if _, exists := base.Extra[k]; !exists {
							log.Debugf("Creating new InferencePool config map from VirtualService %d (namespace=%s)", i+1, config.Namespace)
							base.Extra[k] = v
						}
					}
				}
			}
		}
		// Log final merged InferencePool configs
		if base.Extra != nil {
			if ipConfigs, ok := base.Extra[constants.ConfigExtraPerRouteRuleInferencePoolConfigs].(map[string]kube.InferencePoolRouteRuleConfig); ok {
				log.Debugf("Final merged VirtualService for key %s has %d InferencePool route configs", object.Key, len(ipConfigs))
			}
		}
		sortHTTPRoutes(baseVS.Http)
		base.Name = strings.ReplaceAll(object.Key, "/", "~")
		return ptr.Of(&base)
	}, opts...)
	return finalVirtualServices
}
