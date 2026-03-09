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

// Crediting the kgateway authors for the patterns used in this file, as well as some of the code

package agentgateway

import (
	"fmt"
	"iter"
	"strconv"
	"strings"
	"time"

	"github.com/agentgateway/agentgateway/go/api"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
)

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

type TypedResource struct {
	Kind config.GroupVersionKind
	Name types.NamespacedName
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
	// Create httpRoutes collection
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

	// Create gRPCRoutes collection
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

	// Create TCPRoutes collection
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

	// Create TLSRoutes collection
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

	// Join all the route types into a single collection
	routes := krt.JoinCollection([]krt.Collection[AgwResource]{httpRoutes, grpcRoutes, tcpRoutes, tlsRoutes}, krtopts.WithName("ADPRoutes")...)

	routeAttachments := krt.JoinCollection([]krt.Collection[*RouteAttachment]{
		gatewayRouteAttachmentCountCollection(inputs, httpRouteCol, gvk.HTTPRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, grpcRouteCol, gvk.GRPCRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, tlsRouteCol, gvk.TLSRoute, krtopts),
		gatewayRouteAttachmentCountCollection(inputs, tcpRouteCol, gvk.TCPRoute, krtopts),
	})

	return routes, routeAttachments
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
