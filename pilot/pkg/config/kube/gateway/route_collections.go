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

	"k8s.io/apimachinery/pkg/types"
	// gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	// gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

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

func HTTPRouteCollection(
	httpRoutes krt.Collection[*gateway.HTTPRoute],
	inputs RouteContextInputs,
	opts krt.OptionsBuilder,
) RouteResult[*gateway.HTTPRoute, gateway.HTTPRouteStatus] {
	/* TODO: performance
	 We recompute on each status write. There may be a lot of status writes, especially since we can have multiple controllers owning one object.
	Can we skip updates only to status? maybe but comes with some risk

	Avoid the clones when we merge.

	equals() happens a lot, probably due to status write
	*/
	routeCount := gatewayRouteAttachmentCountCollection(inputs.RouteParents, httpRoutes, gvk.HTTPRoute, opts)
	status, baseVirtualServices := krt.NewStatusManyCollection(httpRoutes, func(krtctx krt.HandlerContext, obj *gateway.HTTPRoute) (
		*gateway.HTTPRouteStatus,
		[]RouteWithKey,
	) {
		status := obj.Status.DeepCopy()
		ctx := inputs.WithCtx(krtctx)
		route := obj.Spec
		parentRefs := extractParentReferenceInfo(ctx.Krt, ctx.RouteParents, route.ParentRefs, route.Hostnames, gvk.HTTPRoute, obj.Namespace)

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
						r.Matches = []gateway.HTTPRouteMatch{*m}
					}
					vs, err := convertHTTPRoute(ctx, r, obj, n, !mesh)
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

		rpResults := slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
			res := RouteParentResult{
				OriginalReference: r.OriginalReference,
				DeniedReason:      r.DeniedReason,
				RouteError:        gwResult.error,
			}
			if r.IsMesh() {
				res.RouteError = meshResult.error
			}
			return res
		})
		status.Parents = createRouteStatus(rpResults, obj.Generation, status.Parents)

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
					routeKey += fmt.Sprintf("/%d", *parent.OriginalReference.Port)
				}
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
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
						parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace)), ctx.Domain)}
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
				name := fmt.Sprintf("%s-%d-%s", obj.Name, count, constants.KubernetesGatewayName)
				sortHTTPRoutes(routes)
				cfg := &config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
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
				virtualServices = append(virtualServices, RouteWithKey{
					Config: cfg,
					Key:    routeKey + "/" + h,
				})
				count++
			}
		}
		return status, virtualServices
	}, opts.WithName("HTTPRoute")...)

	idx := krt.NewIndex(baseVirtualServices, func(o RouteWithKey) []string {
		return []string{o.Key}
	}).AsCollection()
	finalVirtualServices := krt.NewCollection(idx, func(ctx krt.HandlerContext, object krt.IndexObject[string, RouteWithKey]) **config.Config {
		log.Errorf("howardjohn: recompute final %v", object.Key)
		configs := object.Objects
		if len(configs) == 1 {
			return &configs[0].Config
		}
		sortRoutesByCreationTime(configs)
		base := configs[0].DeepCopy()
		baseVS := base.Spec.(*istio.VirtualService)
		for _, config := range configs[1:] {
			thisVS := config.Spec.(*istio.VirtualService)
			baseVS.Http = append(baseVS.Http, thisVS.Http...)
			// append parents
			base.Annotations[constants.InternalParentNames] = fmt.Sprintf("%s,%s",
				base.Annotations[constants.InternalParentNames], config.Annotations[constants.InternalParentNames])
		}
		sortHTTPRoutes(baseVS.Http)
		return ptr.Of(&base)
	}, opts.WithName("HTTPRouteMerged")...)
	return RouteResult[*gateway.HTTPRoute, gateway.HTTPRouteStatus]{
		VirtualServices:  finalVirtualServices,
		RouteAttachments: routeCount,
		Status:           status,
	}
}

//func GRPCRouteCollection(
//	grpcRoutes krt.Collection[*gatewayv1.GRPCRoute],
//	inputs RouteContextInputs,
//	opts krt.OptionsBuilder,
//) RouteResult[*gatewayv1.GRPCRoute, gatewayv1.GRPCRouteStatus] {
//	routeCount := gatewayRouteAttachmentCountCollection(inputs.RouteParents, grpcRoutes, gvk.GRPCRoute, opts)
//	status, baseVirtualServices := krt.NewStatusManyCollection(grpcRoutes, func(krtctx krt.HandlerContext, obj *gatewayv1.GRPCRoute) (
//		*gatewayv1.GRPCRouteStatus,
//		[]config.Config,
//	) {
//		status := obj.Status.DeepCopy()
//		ctx := inputs.WithCtx(krtctx)
//		route := obj.Spec
//		parentRefs := extractParentReferenceInfo(ctx.Krt, ctx.RouteParents, route.ParentRefs, route.Hostnames, gvk.GRPCRoute, obj.Namespace)
//
//		type conversionResult struct {
//			error  *ConfigError
//			routes []*istio.HTTPRoute
//		}
//		convertRules := func(mesh bool) conversionResult {
//			res := conversionResult{}
//			for n, r := range route.Rules {
//				// split the rule to make sure each rule has up to one match
//				matches := slices.Reference(r.Matches)
//				if len(matches) == 0 {
//					matches = append(matches, nil)
//				}
//				for _, m := range matches {
//					if m != nil {
//						r.Matches = []gatewayv1.GRPCRouteMatch{*m}
//					}
//					vs, err := convertGRPCRoute(ctx, r, obj, n, !mesh)
//					// This was a hard error
//					if vs == nil {
//						res.error = err
//						return conversionResult{error: err}
//					}
//					// Got an error but also routes
//					if err != nil {
//						res.error = err
//					}
//
//					res.routes = append(res.routes, vs)
//				}
//			}
//			return res
//		}
//		meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)
//
//		rpResults := slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
//			res := RouteParentResult{
//				OriginalReference: r.OriginalReference,
//				DeniedReason:      r.DeniedReason,
//				RouteError:        gwResult.error,
//			}
//			if r.IsMesh() {
//				res.RouteError = meshResult.error
//			}
//			return res
//		})
//		status.Parents = createRouteStatus(rpResults, obj.Generation, status.Parents)
//
//		count := 0
//		virtualServices := []config.Config{}
//		for _, parent := range filteredReferences(parentRefs) {
//			// for gateway routes, build one VS per gateway+host
//			routeKey := parent.InternalName
//			vsHosts := hostnameToStringList(route.Hostnames)
//			routes := gwResult.routes
//			if parent.IsMesh() {
//				routes = meshResult.routes
//				// for mesh routes, build one VS per namespace/port->host
//				routeKey = obj.Namespace
//				if parent.OriginalReference.Port != nil {
//					routes = augmentPortMatch(routes, *parent.OriginalReference.Port)
//					routeKey += fmt.Sprintf("/%d", *parent.OriginalReference.Port)
//				}
//				ref := types.NamespacedName{
//					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
//					Name:      string(parent.OriginalReference.Name),
//				}
//				if parent.InternalKind == gvk.ServiceEntry {
//					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
//					if ses != nil {
//						vsHosts = ses.Spec.Hosts
//					} else {
//						// TODO: report an error
//						vsHosts = []string{}
//					}
//				} else {
//					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s",
//						parent.OriginalReference.Name, ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace)), ctx.Domain)}
//				}
//			}
//			if len(routes) == 0 {
//				continue
//			}
//			// Create one VS per hostname with a single hostname.
//			// This ensures we can treat each hostname independently, as the spec requires
//			for _, h := range vsHosts {
//				if !parent.hostnameAllowedByIsolation(h) {
//					// TODO: standardize a status message for this upstream and report
//					continue
//				}
//				name := fmt.Sprintf("%s-%d-%s", obj.Name, count, constants.KubernetesGatewayName)
//				annos := routeMeta(obj)
//				annos["TODO"] = fmt.Sprintf(routeKey + "/" + h)
//				virtualServices = append(virtualServices, config.Config{
//					Meta: config.Meta{
//						CreationTimestamp: obj.CreationTimestamp.Time,
//						GroupVersionKind:  gvk.VirtualService,
//						Name:              name,
//						Annotations:       annos,
//						Namespace:         obj.Namespace,
//						Domain:            ctx.Domain,
//					},
//					Spec: &istio.VirtualService{
//						Hosts:    []string{h},
//						Gateways: []string{parent.InternalName},
//						Http:     routes,
//					},
//				})
//				count++
//			}
//		}
//		return status, virtualServices
//	}, opts.WithName("GRPCRoute")...)
//
//	// TODO: this needs to be more efficient. An index as the input perhaps
//	finalVirtualServices := krt.NewManyFromNothing(func(ctx krt.HandlerContext) []config.Config {
//		vs := krt.Fetch(ctx, baseVirtualServices)
//		byKey := slices.Group(vs, func(o config.Config) string {
//			return o.Annotations["TODO"]
//		})
//		final := []config.Config{}
//		for _, configs := range byKey {
//			sortConfigByCreationTime(configs)
//			base := configs[0].DeepCopy()
//			baseVS := base.Spec.(*istio.VirtualService)
//			for _, config := range configs[1:] {
//				thisVS := config.Spec.(*istio.VirtualService)
//				baseVS.Http = append(baseVS.Http, thisVS.Http...)
//				// append parents
//				base.Annotations[constants.InternalParentNames] = fmt.Sprintf("%s,%s",
//					base.Annotations[constants.InternalParentNames], config.Annotations[constants.InternalParentNames])
//			}
//			delete(base.Annotations, "TODO")
//			sortHTTPRoutes(baseVS.Http)
//			final = append(final, base)
//		}
//		return final
//	}, opts.WithName("GRPCRouteMerged")...)
//	return RouteResult[*gatewayv1.GRPCRoute, gatewayv1.GRPCRouteStatus]{
//		VirtualServices:  finalVirtualServices,
//		RouteAttachments: routeCount,
//		Status:           status,
//	}
//}
//
//func TCPRouteCollection(
//	tcpRoutes krt.Collection[*gatewayalpha.TCPRoute],
//	inputs RouteContextInputs,
//	opts krt.OptionsBuilder,
//) RouteResult[*gatewayalpha.TCPRoute, gatewayalpha.TCPRouteStatus] {
//	routeCount := gatewayRouteAttachmentCountCollection(inputs.RouteParents, tcpRoutes, gvk.TCPRoute, opts)
//	status, virtualServices := krt.NewStatusManyCollection(tcpRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TCPRoute) (
//		*gatewayalpha.TCPRouteStatus,
//		[]config.Config,
//	) {
//		status := obj.Status.DeepCopy()
//		ctx := inputs.WithCtx(krtctx)
//		route := obj.Spec
//		parentRefs := extractParentReferenceInfo(ctx.Krt, ctx.RouteParents, route.ParentRefs, nil, gvk.TCPRoute, obj.Namespace)
//
//		type conversionResult struct {
//			error  *ConfigError
//			routes []*istio.TCPRoute
//		}
//		convertRules := func(mesh bool) conversionResult {
//			res := conversionResult{}
//			for _, r := range route.Rules {
//				vs, err := convertTCPRoute(ctx, r, obj, !mesh)
//				// This was a hard error
//				if vs == nil {
//					res.error = err
//					return conversionResult{error: err}
//				}
//				// Got an error but also routes
//				if err != nil {
//					res.error = err
//				}
//				res.routes = append(res.routes, vs)
//			}
//			return res
//		}
//		meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)
//
//		rpResults := slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
//			res := RouteParentResult{
//				OriginalReference: r.OriginalReference,
//				DeniedReason:      r.DeniedReason,
//				RouteError:        gwResult.error,
//			}
//			if r.IsMesh() {
//				res.RouteError = meshResult.error
//			}
//			return res
//		})
//		status.Parents = createRouteStatus(rpResults, obj.Generation, status.Parents)
//
//		vs := []config.Config{}
//		for _, parent := range filteredReferences(parentRefs) {
//			routes := gwResult.routes
//			vsHosts := []string{"*"}
//			if parent.IsMesh() {
//				routes = meshResult.routes
//				if parent.OriginalReference.Port != nil {
//					routes = augmentTCPPortMatch(routes, *parent.OriginalReference.Port)
//				}
//				ref := types.NamespacedName{
//					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
//					Name:      string(parent.OriginalReference.Name),
//				}
//				if parent.InternalKind == gvk.ServiceEntry {
//					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
//					if ses != nil {
//						vsHosts = ses.Spec.Hosts
//					} else {
//						// TODO: report an error
//						vsHosts = []string{}
//					}
//				} else {
//					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, ctx.Domain)}
//				}
//			}
//			for i, host := range vsHosts {
//				name := fmt.Sprintf("%s-tcp-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
//				// Create one VS per hostname with a single hostname.
//				// This ensures we can treat each hostname independently, as the spec requires
//				vs = append(vs, config.Config{
//					Meta: config.Meta{
//						CreationTimestamp: obj.CreationTimestamp.Time,
//						GroupVersionKind:  gvk.VirtualService,
//						Name:              name,
//						Annotations:       routeMeta(obj),
//						Namespace:         obj.Namespace,
//						Domain:            ctx.Domain,
//					},
//					Spec: &istio.VirtualService{
//						// We can use wildcard here since each listener can have at most one route bound to it, so we have
//						// a single VS per Gateway.
//						Hosts:    []string{host},
//						Gateways: []string{parent.InternalName},
//						Tcp:      routes,
//					},
//				})
//			}
//		}
//		return status, vs
//	}, opts.WithName("TCPRoute")...)
//
//	return RouteResult[*gatewayalpha.TCPRoute, gatewayalpha.TCPRouteStatus]{
//		VirtualServices:  virtualServices,
//		RouteAttachments: routeCount,
//		Status:           status,
//	}
//}
//
//func TLSRouteCollection(
//	tlsRoutes krt.Collection[*gatewayalpha.TLSRoute],
//	inputs RouteContextInputs,
//	opts krt.OptionsBuilder,
//) RouteResult[*gatewayalpha.TLSRoute, gatewayalpha.TLSRouteStatus] {
//	routeCount := gatewayRouteAttachmentCountCollection(inputs.RouteParents, tlsRoutes, gvk.TLSRoute, opts)
//	status, virtualServices := krt.NewStatusManyCollection(tlsRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TLSRoute) (
//		*gatewayalpha.TLSRouteStatus,
//		[]config.Config,
//	) {
//		status := obj.Status.DeepCopy()
//		ctx := inputs.WithCtx(krtctx)
//		route := obj.Spec
//		parentRefs := extractParentReferenceInfo(ctx.Krt, inputs.RouteParents, route.ParentRefs, nil, gvk.TLSRoute, obj.Namespace)
//
//		type conversionResult struct {
//			error  *ConfigError
//			routes []*istio.TLSRoute
//		}
//		convertRules := func(mesh bool) conversionResult {
//			res := conversionResult{}
//			for _, r := range route.Rules {
//				vs, err := convertTLSRoute(ctx, r, obj, !mesh)
//				// This was a hard error
//				if vs == nil {
//					res.error = err
//					return conversionResult{error: err}
//				}
//				// Got an error but also routes
//				if err != nil {
//					res.error = err
//				}
//				res.routes = append(res.routes, vs)
//			}
//			return res
//		}
//		meshResult, gwResult := buildMeshAndGatewayRoutes(parentRefs, convertRules)
//
//		rpResults := slices.Map(parentRefs, func(r routeParentReference) RouteParentResult {
//			res := RouteParentResult{
//				OriginalReference: r.OriginalReference,
//				DeniedReason:      r.DeniedReason,
//				RouteError:        gwResult.error,
//			}
//			if r.IsMesh() {
//				res.RouteError = meshResult.error
//			}
//			return res
//		})
//		status.Parents = createRouteStatus(rpResults, obj.Generation, status.Parents)
//
//		vs := []config.Config{}
//		for _, parent := range filteredReferences(parentRefs) {
//			routes := gwResult.routes
//			vsHosts := hostnameToStringList(route.Hostnames)
//			if parent.IsMesh() {
//				routes = meshResult.routes
//				ref := types.NamespacedName{
//					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
//					Name:      string(parent.OriginalReference.Name),
//				}
//				if parent.InternalKind == gvk.ServiceEntry {
//					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(ref.String())))
//					if ses != nil {
//						vsHosts = ses.Spec.Hosts
//					} else {
//						// TODO: report an error
//						vsHosts = []string{}
//					}
//				} else {
//					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, ctx.Domain)}
//				}
//				routes = augmentTLSPortMatch(routes, parent.OriginalReference.Port, vsHosts)
//			}
//			for i, host := range vsHosts {
//				name := fmt.Sprintf("%s-tls-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
//				filteredRoutes := routes
//				if parent.IsMesh() {
//					filteredRoutes = compatibleRoutesForHost(routes, host)
//				}
//				// Create one VS per hostname with a single hostname.
//				// This ensures we can treat each hostname independently, as the spec requires
//				vs = append(vs, config.Config{
//					Meta: config.Meta{
//						CreationTimestamp: obj.CreationTimestamp.Time,
//						GroupVersionKind:  gvk.VirtualService,
//						Name:              name,
//						Annotations:       routeMeta(obj),
//						Namespace:         obj.Namespace,
//						Domain:            ctx.Domain,
//					},
//					Spec: &istio.VirtualService{
//						Hosts:    []string{host},
//						Gateways: []string{parent.InternalName},
//						Tls:      filteredRoutes,
//					},
//				})
//			}
//		}
//		return status, vs
//	}, opts.WithName("TLSRoute")...)
//	return RouteResult[*gatewayalpha.TLSRoute, gatewayalpha.TLSRouteStatus]{
//		VirtualServices:  virtualServices,
//		RouteAttachments: routeCount,
//		Status:           status,
//	}
//}

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

type RouteResult[I, IStatus any] struct {
	VirtualServices  krt.Collection[*config.Config]
	RouteAttachments krt.Collection[RouteAttachment]
	Status           krt.Collection[krt.ObjectWithStatus[I, IStatus]]
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

func gatewayRouteAttachmentCountCollection[T controllers.Object](
	parentsCollection RouteParents,
	col krt.Collection[T],
	kind config.GroupVersionKind,
	opts krt.OptionsBuilder,
) krt.Collection[RouteAttachment] {
	return krt.NewManyCollection(col, func(krtctx krt.HandlerContext, obj T) []RouteAttachment {
		from := TypedResource{
			Kind: kind,
			Name: config.NamespacedName(obj),
		}
		parents, hostnames := GetCommonRouteInfo(obj)
		parentRefs := extractParentReferenceInfo(krtctx, parentsCollection, parents, hostnames, kind, obj.GetNamespace())
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
