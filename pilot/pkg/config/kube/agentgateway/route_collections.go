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

package agentgateway

import (
	"fmt"
	"iter"
	"strings"

	"github.com/agentgateway/agentgateway/go/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

type TypedNamespacedName struct {
	types.NamespacedName
	Kind kind.Kind
}

// AgwRouteCollection creates the collection of translated Routes
func AgwRouteCollection(
	queue *status.StatusCollections,
	httpRouteCol krt.Collection[*gatewayv1.HTTPRoute],
	grpcRouteCol krt.Collection[*gatewayv1.GRPCRoute],
	tcpRouteCol krt.Collection[*gatewayalpha.TCPRoute],
	tlsRouteCol krt.Collection[*gatewayalpha.TLSRoute],
	inputs RouteContextInputs,
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	krtopts krt.OptionsBuilder,
) (krt.Collection[AgwResource], krt.Collection[*RouteAttachment]) {
	httpRouteStatus, httpRoutes := createRouteCollection(httpRouteCol, inputs, krtopts, "HTTPRoutes",
		func(ctx RouteContext, obj *gatewayv1.HTTPRoute) (RouteContext, iter.Seq2[AgwRoute, *condition]) {
			route := obj.Spec
			return ctx, func(yield func(AgwRoute, *condition) bool) {
				for n, r := range route.Rules {
					// split the rule to make sure each rule has up to one match
					matches := slices.Reference(r.Matches)
					if len(matches) == 0 {
						matches = append(matches, nil)
					}
					for idx, m := range matches {
						if m != nil {
							r.Matches = []gatewayv1.HTTPRouteMatch{*m}
						}
						res, err := ConvertHTTPRouteToAgw(ctx, r, obj, n, idx)
						if !yield(AgwRoute{Route: res}, err) {
							return
						}
					}
				}
			}
		}, func(status gatewayv1.RouteStatus) gatewayv1.HTTPRouteStatus {
			return gatewayv1.HTTPRouteStatus{RouteStatus: status}
		})
	status.RegisterStatus(queue, httpRouteStatus, GetStatus, tagWatcher.AccessUnprotected())

	grpcRouteStatus, grpcRoutes := createRouteCollection(grpcRouteCol, inputs, krtopts, "GRPCRoutes",
		func(ctx RouteContext, obj *gatewayv1.GRPCRoute) (RouteContext, iter.Seq2[AgwRoute, *condition]) {
			route := obj.Spec
			return ctx, func(yield func(AgwRoute, *condition) bool) {
				for n, r := range route.Rules {
					// Convert the entire rule with all matches at once
					res, err := ConvertGRPCRouteToAgw(ctx, r, obj, n)
					if !yield(AgwRoute{Route: res}, err) {
						return
					}
				}
			}
		}, func(status gatewayv1.RouteStatus) gatewayv1.GRPCRouteStatus {
			return gatewayv1.GRPCRouteStatus{RouteStatus: status}
		})
	status.RegisterStatus(queue, grpcRouteStatus, GetStatus, tagWatcher.AccessUnprotected())

	tcpRouteStatus, tcpRoutes := createTCPRouteCollection(tcpRouteCol, inputs, krtopts, "TCPRoutes",
		func(ctx RouteContext, obj *gatewayalpha.TCPRoute) (RouteContext, iter.Seq2[AgwTCPRoute, *condition]) {
			route := obj.Spec
			return ctx, func(yield func(AgwTCPRoute, *condition) bool) {
				for n, r := range route.Rules {
					// Convert the entire rule with all matches at once
					res, err := ConvertTCPRouteToAgw(ctx, r, obj, n)
					if !yield(AgwTCPRoute{TCPRoute: res}, err) {
						return
					}
				}
			}
		}, func(status gatewayv1.RouteStatus) gatewayalpha.TCPRouteStatus {
			return gatewayalpha.TCPRouteStatus{RouteStatus: status}
		})
	status.RegisterStatus(queue, tcpRouteStatus, GetStatus, tagWatcher.AccessUnprotected())

	tlsRouteStatus, tlsRoutes := createTCPRouteCollection(tlsRouteCol, inputs, krtopts, "TLSRoutes",
		func(ctx RouteContext, obj *gatewayalpha.TLSRoute) (RouteContext, iter.Seq2[AgwTCPRoute, *condition]) {
			route := obj.Spec
			return ctx, func(yield func(AgwTCPRoute, *condition) bool) {
				for n, r := range route.Rules {
					// Convert the entire rule with all matches at once
					res, err := ConvertTLSRouteToAgw(ctx, r, obj, n)
					if !yield(AgwTCPRoute{TCPRoute: res}, err) {
						return
					}
				}
			}
		}, func(status gatewayv1.RouteStatus) gatewayalpha.TLSRouteStatus {
			return gatewayalpha.TLSRouteStatus{RouteStatus: status}
		})
	status.RegisterStatus(queue, tlsRouteStatus, GetStatus, tagWatcher.AccessUnprotected())

	routes := krt.JoinCollection([]krt.Collection[AgwResource]{httpRoutes, grpcRoutes, tcpRoutes, tlsRoutes}, krtopts.WithName("ADPRoutes")...)

	routeAttachments := krt.JoinCollection([]krt.Collection[*RouteAttachment]{
		gatewayRouteAttachmentCountCollection(inputs, httpRouteCol, gvk.HTTPRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, grpcRouteCol, gvk.GRPCRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, tlsRouteCol, gvk.TLSRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, tcpRouteCol, gvk.TCPRoute, krtopts),
	})

	return routes, routeAttachments
}

// Simplified TCP route collection function (plugins parameter removed)
func createTCPRouteCollection[T controllers.Object, ST any](
	routeCol krt.Collection[T],
	inputs RouteContextInputs,
	krtopts krt.OptionsBuilder,
	collectionName string,
	translator func(ctx RouteContext, obj T) (RouteContext, iter.Seq2[AgwTCPRoute, *condition]),
	buildStatus func(status gatewayv1.RouteStatus) ST,
) (
	krt.StatusCollection[T, ST],
	krt.Collection[AgwResource],
) {
	return createRouteCollectionGeneric(
		routeCol,
		inputs,
		krtopts,
		collectionName,
		translator,
		func(e AgwTCPRoute, parent RouteParentReference) *api.Resource {
			inner := protomarshal.Clone(e.TCPRoute)
			_, name, _ := strings.Cut(parent.InternalName, "/")
			inner.ListenerKey = name
			if sec := string(parent.ParentSection); sec != "" {
				inner.Key = inner.GetKey() + "." + sec
			} else {
				inner.Key = inner.GetKey()
			}
			return ToAgwResource(AgwTCPRoute{TCPRoute: inner})
		},
		buildStatus,
	)
}

type ConversionResult[O any] struct {
	Error  *condition
	Routes []O
}

// TODO(jaellio): Handle Route conditions
// ProcessParentReferences processes filtered parent references and builds resources per gateway.
// It emits exactly one ParentStatus per Gateway (aggregate across listeners).
// If no listeners are allowed, the Accepted reason is:
//   - NotAllowedByListeners  => when the parent Gateway is cross-namespace w.r.t. the route
//   - NoMatchingListenerHostname => otherwise
func ProcessParentReferences[T any](
	parentRefs []RouteParentReference,
	gwResult ConversionResult[T],
	routeNN types.NamespacedName, // <-- route namespace/name so we can detect cross-NS parents
	// routeReporter reporter.RouteReporter,
	resourceMapper func(T, RouteParentReference) *api.Resource,
) []AgwResource {
	resources := make([]AgwResource, 0, len(parentRefs))

	// Build the "allowed" set from FilteredReferences (listener-scoped).
	allowed := make(map[string]struct{})
	for _, p := range FilteredReferences(parentRefs) {
		k := fmt.Sprintf("%s/%s/%s/%s", p.ParentKey.Namespace, p.ParentKey.Name, p.ParentKey.Kind, string(p.ParentSection))
		allowed[k] = struct{}{}
	}

	// Aggregate per Gateway for status; also track whether any raw parent was cross-namespace.
	type gwAgg struct {
		anyAllowed bool
		rep        RouteParentReference
	}
	agg := make(map[types.NamespacedName]*gwAgg)
	crossNS := sets.New[types.NamespacedName]()
	denied := make(map[types.NamespacedName]*ParentError)

	for _, p := range parentRefs {
		gwNN := p.ParentGateway
		if _, ok := agg[gwNN]; !ok {
			agg[gwNN] = &gwAgg{anyAllowed: false, rep: p}
		}
		if p.ParentKey.Namespace != routeNN.Namespace {
			crossNS.Insert(gwNN)
		}
		if p.DeniedReason != nil {
			denied[gwNN] = p.DeniedReason
		}
	}

	// If conversion (backend/filter resolution) failed, ResolvedRefs=False for all parents.
	// resolvedOK := (gwResult.Error == nil)

	// Consider each raw parentRef (listener-scoped) for mapping.
	for _, parent := range parentRefs {
		gwNN := parent.ParentGateway
		listener := string(parent.ParentSection)
		keyStr := fmt.Sprintf("%s/%s/%s/%s", parent.ParentKey.Namespace, parent.ParentKey.Name, parent.ParentKey.Kind, listener)
		_, isAllowed := allowed[keyStr]

		if isAllowed {
			if a := agg[gwNN]; a != nil {
				a.anyAllowed = true
			}
		}
		// Only attach resources when listener is allowed. Even if ResolvedRefs is false,
		// we still attach so any DirectResponse policy can return 5xx as required.
		if !isAllowed {
			continue
		}
		routes := gwResult.Routes
		for i := range routes {
			if r := resourceMapper(routes[i], parent); r != nil {
				resources = append(resources, ToResourceForGateway(gwNN, r))
			}
		}
	}

	// TODO(jaellio): generate status per parent/gateway
	return resources
}

// buildAttachedRoutesMapAllowed is the same as buildAttachedRoutesMap,
// but only for already-evaluated, allowed parentRefs.
func buildAttachedRoutesMapAllowed(
	allowedParents []RouteParentReference,
	routeNN types.NamespacedName,
) map[types.NamespacedName]map[string]uint {
	attached := make(map[types.NamespacedName]map[string]uint)
	type attachKey struct {
		gw       types.NamespacedName
		listener string
		route    types.NamespacedName
	}
	seen := make(map[attachKey]struct{})

	for _, parent := range allowedParents {
		if parent.ParentKey.Kind != schema.GroupVersionKind(gvk.Gateway) {
			continue
		}
		gw := types.NamespacedName{Namespace: parent.ParentKey.Namespace, Name: parent.ParentKey.Name}
		lis := string(parent.ParentSection)

		k := attachKey{gw: gw, listener: lis, route: routeNN}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}

		if attached[gw] == nil {
			attached[gw] = make(map[string]uint)
		}
		attached[gw][lis]++
	}
	return attached
}

// ListenersPerGateway returns the set of listener sectionNames referenced for each parent Gateway,
// regardless of whether they are allowed.
func ListenersPerGateway(parentRefs []RouteParentReference) map[types.NamespacedName]map[string]struct{} {
	l := make(map[types.NamespacedName]map[string]struct{})
	for _, p := range parentRefs {
		if p.ParentKey.Kind != gvk.Gateway.Kubernetes() {
			continue
		}
		gw := types.NamespacedName{Namespace: p.ParentKey.Namespace, Name: p.ParentKey.Name}
		if l[gw] == nil {
			l[gw] = make(map[string]struct{})
		}
		l[gw][string(p.ParentSection)] = struct{}{}
	}
	return l
}

// EnsureZeroes pre-populates AttachedRoutes with explicit 0 entries for every referenced listener,
// so writers that "replace" rather than "merge" will correctly set zero.
func EnsureZeroes(
	attached map[types.NamespacedName]map[string]uint,
	ln map[types.NamespacedName]map[string]struct{},
) {
	for gw, set := range ln {
		if attached[gw] == nil {
			attached[gw] = make(map[string]uint)
		}
		for lis := range set {
			if _, ok := attached[gw][lis]; !ok {
				attached[gw][lis] = 0
			}
		}
	}
}

// Generic function that handles the common logic
func createRouteCollectionGeneric[T controllers.Object, R comparable, ST any](
	routeCol krt.Collection[T],
	inputs RouteContextInputs,
	krtopts krt.OptionsBuilder,
	collectionName string,
	translator func(ctx RouteContext, obj T) (RouteContext, iter.Seq2[R, *condition]),
	resourceTransformer func(route R, parent RouteParentReference) *api.Resource,
	buildStatus func(status gatewayv1.RouteStatus) ST,
) (
	krt.StatusCollection[T, ST],
	krt.Collection[AgwResource],
) {
	return krt.NewStatusManyCollection(routeCol, func(krtctx krt.HandlerContext, obj T) (*ST, []AgwResource) {
		ctx := inputs.WithCtx(krtctx)

		// Apply route-specific preprocessing and get the translator
		ctx, translatorSeq := translator(ctx, obj)

		parentRefs, gwResult := computeRoute(ctx, obj, func(obj T) iter.Seq2[R, *condition] {
			return translatorSeq
		})

		// gateway -> section name -> route count
		routeNN := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		ln := ListenersPerGateway(parentRefs)
		allowedParents := FilteredReferences(parentRefs)
		attachedRoutes := buildAttachedRoutesMapAllowed(allowedParents, routeNN)
		EnsureZeroes(attachedRoutes, ln)

		resources := ProcessParentReferences[R](
			parentRefs,
			gwResult,
			routeNN,
			resourceTransformer,
		)
		// TODO(jaellio)
		// status := BuildRouteStatusWithParentRefDefaulting(context.Background(), obj, inputs.ControllerName, true)
		// return ptr.Of(buildStatus(*status)), resources
		return nil, resources
	}, krtopts.WithName(collectionName)...)
}

// Simplified HTTP route collection function
func createRouteCollection[T controllers.Object, ST any](
	routeCol krt.Collection[T],
	inputs RouteContextInputs,
	krtopts krt.OptionsBuilder,
	collectionName string,
	translator func(ctx RouteContext, obj T) (RouteContext, iter.Seq2[AgwRoute, *condition]),
	buildStatus func(status gatewayv1.RouteStatus) ST,
) (
	krt.StatusCollection[T, ST],
	krt.Collection[AgwResource],
) {
	return createRouteCollectionGeneric(
		routeCol,
		inputs,
		krtopts,
		collectionName,
		translator,
		func(e AgwRoute, parent RouteParentReference) *api.Resource {
			// safety: a shallow clone is ok because we only modify a top level field (Key)
			inner := protomarshal.ShallowClone(e.Route)
			_, name, _ := strings.Cut(parent.InternalName, "/")
			inner.ListenerKey = name
			if sec := string(parent.ParentSection); sec != "" {
				inner.Key = inner.GetKey() + "." + sec
			} else {
				inner.Key = inner.GetKey()
			}
			return ToAgwResource(AgwRoute{Route: inner})
		},
		buildStatus,
	)
}

// IsNil works around comparing generic types
func IsNil[O comparable](o O) bool {
	var t O
	return o == t
}

// computeRoute holds the common route building logic shared amongst all types
func computeRoute[T controllers.Object, O comparable](ctx RouteContext, obj T, translator func(
	obj T,
) iter.Seq2[O, *condition],
) ([]RouteParentReference, ConversionResult[O]) {
	parentRefs := extractParentReferenceInfo(ctx, ctx.RouteParents, obj)

	convertRules := func() ConversionResult[O] {
		res := ConversionResult[O]{}
		for vs, err := range translator(obj) {
			if err != nil && IsNil(vs) {
				res.Error = err
				return ConversionResult[O]{Error: err}
			}
			if err != nil {
				res.Error = err
			}
			res.Routes = append(res.Routes, vs)
		}
		return res
	}
	gwResult := buildGatewayRoutes(convertRules)

	return parentRefs, gwResult
}

// buildGatewayRoutes contains common logic to build a set of Routes with v1/alpha2 semantics
func buildGatewayRoutes[T any](convertRules func() T) T {
	return convertRules()
}

// RouteContext defines a common set of inputs to a route collection. This should be built once per route translation and
// not shared outside of that.
// The embedded RouteContextInputs is typically based into a collection, then translated to a RouteContext with RouteContextInputs.WithCtx().
type RouteContext struct {
	Krt krt.HandlerContext
	RouteContextInputs
}

type RouteContextInputs struct {
	Grants         gatewaycommon.ReferenceGrants
	RouteParents   RouteParents
	DomainSuffix   string
	Services       krt.Collection[*corev1.Service]
	Namespaces     krt.Collection[*corev1.Namespace]
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry]
	InferencePools krt.Collection[*inferencev1.InferencePool]
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

// RouteResult holds the result of a route collection
type RouteResult[I controllers.Object, IStatus any] struct {
	// VirtualServices are the primary output that configures the internal routing logic
	VirtualServices krt.Collection[*config.Config]
	// RouteAttachments holds information about parent attachment to routes, used for computed the `attachedRoutes` count.
	RouteAttachments krt.Collection[RouteAttachment]
	// Status stores the status reports for the incoming object
	Status krt.StatusCollection[I, IStatus]
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
) krt.Collection[*RouteAttachment] {
	return krt.NewManyCollection(col, func(krtctx krt.HandlerContext, obj T) []*RouteAttachment {
		ctx := inputs.WithCtx(krtctx)
		from := TypedResource{
			Kind: kind,
			Name: config.NamespacedName(obj),
		}

		parentRefs := extractParentReferenceInfo(ctx, inputs.RouteParents, obj)
		return slices.MapFilter(FilteredReferences(parentRefs), func(e RouteParentReference) **RouteAttachment {
			if e.ParentKey.Kind != gvk.KubernetesGateway.Kubernetes() {
				return nil
			}
			return ptr.Of(&RouteAttachment{
				From: from,
				To: types.NamespacedName{
					Name:      e.ParentKey.Name,
					Namespace: e.ParentKey.Namespace,
				},
				ListenerName: string(e.ParentSection),
			})
		})
	}, opts.WithName(kind.Kind+"/count")...)
}
