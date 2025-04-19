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
	"strings"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	kubeconfig "istio.io/istio/pkg/config/gateway/kube"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
)

type Gateway struct {
	*config.Config
	parent     parentKey
	parentInfo parentInfo
	Valid      bool
}

func (g Gateway) ResourceName() string {
	return config.NamespacedName(g.Config).String()
}

func (g Gateway) Equals(other Gateway) bool {
	return g.Config.Equals(other.Config) &&
		g.Valid == other.Valid // TODO: ok to ignore parent/parentInfo?
}

func GatewayCollection(
	gateways krt.Collection[*gateway.Gateway],
	gatewayClasses krt.Collection[GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants ReferenceGrants,
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]],
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gateway.Gateway, gateway.GatewayStatus],
	krt.Collection[Gateway],
) {
	statusCol, gw := krt.NewStatusManyCollection(gateways, func(ctx krt.HandlerContext, obj *gateway.Gateway) (*gateway.GatewayStatus, []Gateway) {
		// We currently depend on service discovery information not know to krt; mark we depend on it.
		context := gatewayContext.Get(ctx).Load()
		if context == nil {
			return nil, nil
		}
		if !tagWatcher.Get(ctx).IsMine(obj.ObjectMeta) {
			return nil, nil
		}
		result := []Gateway{}
		kgw := obj.Spec
		status := obj.Status.DeepCopy()
		class := fetchClass(ctx, gatewayClasses, kgw.GatewayClassName)
		if class == nil {
			return nil, nil
		}
		controllerName := class.Controller
		classInfo, f := classInfos[controllerName]
		if !f {
			return nil, nil
		}
		if classInfo.disableRouteGeneration {
			reportUnmanagedGatewayStatus(status, obj)
			// We found it, but don't want to handle this class
			return status, nil
		}
		servers := []*istio.Server{}

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, err := extractGatewayServices(domainSuffix, obj, classInfo)
		if len(gatewayServices) == 0 && err != nil {
			// Short circuit if its a hard failure
			reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, err)
			return status, nil
		}

		for i, l := range kgw.Listeners {
			server, programmed := buildListener(ctx, secrets, grants, namespaces, obj, status, l, i, controllerName)

			servers = append(servers, server)
			if controllerName == constants.ManagedGatewayMeshController || controllerName == constants.ManagedGatewayEastWestController {
				// Waypoint and ambient e/w don't actually convert the routes to VirtualServices
				// TODO: Maybe E/W gateway should for non 15008 ports for backwards compat?
				continue
			}
			meta := parentMeta(obj, &l.Name)
			meta[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway
			meta[model.InternalGatewayServiceAnnotation] = strings.Join(gatewayServices, ",")

			// Each listener generates an Istio Gateway with a single Server. This allows binding to a specific listener.
			gatewayConfig := config.Config{
				Meta: config.Meta{
					CreationTimestamp: obj.CreationTimestamp.Time,
					GroupVersionKind:  gvk.Gateway,
					Name:              kubeconfig.InternalGatewayName(obj.Name, string(l.Name)),
					Annotations:       meta,
					Namespace:         obj.Namespace,
					Domain:            domainSuffix,
				},
				Spec: &istio.Gateway{
					Servers: []*istio.Server{server},
				},
			}

			allowed, _ := generateSupportedKinds(l)
			ref := parentKey{
				Kind:      gvk.KubernetesGateway,
				Name:      obj.Name,
				Namespace: obj.Namespace,
			}
			pri := parentInfo{
				InternalName:     obj.Namespace + "/" + gatewayConfig.Name,
				AllowedKinds:     allowed,
				Hostnames:        server.Hosts,
				OriginalHostname: string(ptr.OrEmpty(l.Hostname)),
				SectionName:      l.Name,
				Port:             l.Port,
				Protocol:         l.Protocol,
			}

			res := Gateway{
				Config:     &gatewayConfig,
				Valid:      programmed,
				parent:     ref,
				parentInfo: pri,
			}
			result = append(result, res)
		}

		reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, err)
		return status, result
	}, opts.WithName("KubernetesGateway")...)

	return statusCol, gw
}

// FinalGatewayStatusCollection finalizes a Gateway status. There is a circular logic between Gateways and Routes to determine
// the attachedRoute count, so we first build a partial Gateway status, then once routes are computed we finalize it with
// the attachedRoute count.
func FinalGatewayStatusCollection(
	gatewayStatuses krt.StatusCollection[*gateway.Gateway, gateway.GatewayStatus],
	routeAttachments krt.Collection[RouteAttachment],
	routeAttachmentsIndex krt.Index[types.NamespacedName, RouteAttachment],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gateway.Gateway, gateway.GatewayStatus] {
	return krt.NewCollection(
		gatewayStatuses,
		func(ctx krt.HandlerContext, i krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus]) *krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus] {
			tcpRoutes := krt.Fetch(ctx, routeAttachments, krt.FilterIndex(routeAttachmentsIndex, config.NamespacedName(i.Obj)))
			counts := map[string]int32{}
			for _, r := range tcpRoutes {
				counts[r.ListenerName]++
			}
			status := i.Status.DeepCopy()
			for i, s := range status.Listeners {
				s.AttachedRoutes = counts[string(s.Name)]
				status.Listeners[i] = s
			}
			return &krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus]{
				Obj:    i.Obj,
				Status: *status,
			}
		}, opts.WithName("GatewayFinalStatus")...)
}

// RouteParents holds information about things routes can reference as parents.
type RouteParents struct {
	gateways     krt.Collection[Gateway]
	gatewayIndex krt.Index[parentKey, Gateway]
}

func (p RouteParents) fetch(ctx krt.HandlerContext, pk parentKey) []*parentInfo {
	if pk == meshParentKey {
		// Special case
		return []*parentInfo{
			{
				InternalName: "mesh",
				// Mesh has no configurable AllowedKinds, so allow all supported
				AllowedKinds: []gateway.RouteGroupKind{
					{Group: (*gateway.Group)(ptr.Of(gvk.HTTPRoute.Group)), Kind: gateway.Kind(gvk.HTTPRoute.Kind)},
					{Group: (*gateway.Group)(ptr.Of(gvk.GRPCRoute.Group)), Kind: gateway.Kind(gvk.GRPCRoute.Kind)},
					{Group: (*gateway.Group)(ptr.Of(gvk.TCPRoute.Group)), Kind: gateway.Kind(gvk.TCPRoute.Kind)},
					{Group: (*gateway.Group)(ptr.Of(gvk.TLSRoute.Group)), Kind: gateway.Kind(gvk.TLSRoute.Kind)},
				},
			},
		}
	}
	return slices.Map(krt.Fetch(ctx, p.gateways, krt.FilterIndex(p.gatewayIndex, pk)), func(gw Gateway) *parentInfo {
		return &gw.parentInfo
	})
}

func BuildRouteParents(
	gateways krt.Collection[Gateway],
) RouteParents {
	idx := krt.NewIndex(gateways, func(o Gateway) []parentKey {
		return []parentKey{o.parent}
	})
	return RouteParents{
		gateways:     gateways,
		gatewayIndex: idx,
	}
}
