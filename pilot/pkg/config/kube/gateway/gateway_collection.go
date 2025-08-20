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
	"strings"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

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
	*config.Config `json:"config"`
	Parent         parentKey  `json:"parent"`
	ParentInfo     parentInfo `json:"parentInfo"`
	Valid          bool       `json:"valid"`
}

func (g Gateway) ResourceName() string {
	return config.NamespacedName(g.Config).String()
}

func (g Gateway) Equals(other Gateway) bool {
	return g.Config.Equals(other.Config) &&
		g.Valid == other.Valid // TODO: ok to ignore parent/parentInfo?
}

type ListenerSet struct {
	*config.Config `json:"config"`
	Parent         parentKey            `json:"parent"`
	ParentInfo     parentInfo           `json:"parentInfo"`
	GatewayParent  types.NamespacedName `json:"gatewayParent"`
	Valid          bool                 `json:"valid"`
}

func (g ListenerSet) ResourceName() string {
	return config.NamespacedName(g.Config).Name
}

func (g ListenerSet) Equals(other ListenerSet) bool {
	return g.Config.Equals(other.Config) &&
		g.GatewayParent == other.GatewayParent &&
		g.Valid == other.Valid // TODO: ok to ignore parent/parentInfo?
}

func ListenerSetCollection(
	listenerSets krt.Collection[*gatewayx.XListenerSet],
	gateways krt.Collection[*gateway.Gateway],
	gatewayClasses krt.Collection[GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants ReferenceGrants,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]],
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gatewayx.XListenerSet, gatewayx.ListenerSetStatus],
	krt.Collection[ListenerSet],
) {
	statusCol, gw := krt.NewStatusManyCollection(listenerSets,
		func(ctx krt.HandlerContext, obj *gatewayx.XListenerSet) (*gatewayx.ListenerSetStatus, []ListenerSet) {
			// We currently depend on service discovery information not know to krt; mark we depend on it.
			context := gatewayContext.Get(ctx).Load()
			if context == nil {
				return nil, nil
			}
			if !tagWatcher.Get(ctx).IsMine(obj.ObjectMeta) {
				return nil, nil
			}
			result := []ListenerSet{}
			ls := obj.Spec
			status := obj.Status.DeepCopy()

			p := ls.ParentRef
			if normalizeReference(p.Group, p.Kind, gvk.KubernetesGateway) != gvk.KubernetesGateway {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			pns := ptr.OrDefault(p.Namespace, gatewayx.Namespace(obj.Namespace))
			parentGwObj := ptr.Flatten(krt.FetchOne(ctx, gateways, krt.FilterKey(string(pns)+"/"+string(p.Name))))
			if parentGwObj == nil {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			class := fetchClass(ctx, gatewayClasses, parentGwObj.Spec.GatewayClassName)
			if class == nil {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			controllerName := class.Controller
			classInfo, f := classInfos[controllerName]
			if !f {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}
			if !classInfo.supportsListenerSet {
				reportUnsupportedListenerSet(class.Name, status, obj)
				return status, nil
			}

			if !namespaceAcceptedByAllowListeners(obj.Namespace, parentGwObj, func(s string) *corev1.Namespace {
				return ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(s)))
			}) {
				reportNotAllowedListenerSet(status, obj)
				return status, nil
			}

			gatewayServices, err := extractGatewayServices(domainSuffix, parentGwObj, classInfo)
			if len(gatewayServices) == 0 && err != nil {
				// Short circuit if it's a hard failure
				reportListenerSetStatus(context, parentGwObj, obj, status, gatewayServices, nil, err)
				return status, nil
			}

			servers := []*istio.Server{}
			for i, l := range ls.Listeners {
				port, portErr := detectListenerPortNumber(l)
				l.Port = port
				standardListener := convertListenerSetToListener(l)
				originalStatus := slices.Map(status.Listeners, convertListenerSetStatusToStandardStatus)
				server, updatedStatus, programmed := buildListener(ctx, configMaps, secrets, grants, namespaces,
					obj, originalStatus, standardListener, i, controllerName, portErr)
				status.Listeners = slices.Map(updatedStatus, convertStandardStatusToListenerSetStatus(l))

				servers = append(servers, server)
				if controllerName == constants.ManagedGatewayMeshController || controllerName == constants.ManagedGatewayEastWestController {
					// Waypoint doesn't actually convert the routes to VirtualServices
					continue
				}
				meta := parentMeta(obj, &l.Name)
				meta[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway
				meta[model.InternalGatewayServiceAnnotation] = strings.Join(gatewayServices, ",")
				meta[constants.InternalParentNamespace] = parentGwObj.Namespace

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

				allowed, _ := generateSupportedKinds(standardListener)
				ref := parentKey{
					Kind:      gvk.XListenerSet,
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

				res := ListenerSet{
					Config:        &gatewayConfig,
					Valid:         programmed,
					Parent:        ref,
					GatewayParent: config.NamespacedName(parentGwObj),
					ParentInfo:    pri,
				}
				result = append(result, res)
			}

			reportListenerSetStatus(context, parentGwObj, obj, status, gatewayServices, servers, err)
			return status, result
		}, opts.WithName("ListenerSets")...)

	return statusCol, gw
}

func GatewayCollection(
	gateways krt.Collection[*gateway.Gateway],
	listenerSets krt.Collection[ListenerSet],
	gatewayClasses krt.Collection[GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants ReferenceGrants,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]],
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gateway.Gateway, gateway.GatewayStatus],
	krt.Collection[Gateway],
) {
	listenerIndex := krt.NewIndex(listenerSets, "gatewayParent", func(o ListenerSet) []types.NamespacedName {
		return []types.NamespacedName{o.GatewayParent}
	})
	statusCol, gw := krt.NewStatusManyCollection(gateways, func(ctx krt.HandlerContext, obj *gateway.Gateway) (*gateway.GatewayStatus, []Gateway) {
		// We currently depend on service discovery information not known to krt; mark we depend on it.
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
			reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, 0, err)
			return status, nil
		}

		for i, l := range kgw.Listeners {
			server, updatedStatus, programmed := buildListener(ctx, configMaps, secrets, grants, namespaces, obj, status.Listeners, l, i, controllerName, nil)
			status.Listeners = updatedStatus

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
				Parent:     ref,
				ParentInfo: pri,
			}
			result = append(result, res)
		}
		listenersFromSets := krt.Fetch(ctx, listenerSets, krt.FilterIndex(listenerIndex, config.NamespacedName(obj)))
		for _, ls := range listenersFromSets {
			servers = append(servers, ls.Config.Spec.(*istio.Gateway).Servers...)
			result = append(result, Gateway{
				Config:     ls.Config,
				Parent:     ls.Parent,
				ParentInfo: ls.ParentInfo,
				Valid:      ls.Valid,
			})
		}

		reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, len(listenersFromSets), err)
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
		return &gw.ParentInfo
	})
}

func BuildRouteParents(
	gateways krt.Collection[Gateway],
) RouteParents {
	idx := krt.NewIndex(gateways, "parent", func(o Gateway) []parentKey {
		return []parentKey{o.Parent}
	})
	return RouteParents{
		gateways:     gateways,
		gatewayIndex: idx,
	}
}

func detectListenerPortNumber(l gatewayx.ListenerEntry) (gatewayx.PortNumber, error) {
	if l.Port != 0 {
		return l.Port, nil
	}
	switch l.Protocol {
	case gatewayv1.HTTPProtocolType:
		return 80, nil
	case gatewayv1.HTTPSProtocolType:
		return 443, nil
	}
	return 0, fmt.Errorf("protocol %v requires a port to be set", l.Protocol)
}

func convertStandardStatusToListenerSetStatus(l gatewayx.ListenerEntry) func(e gateway.ListenerStatus) gatewayx.ListenerEntryStatus {
	return func(e gateway.ListenerStatus) gatewayx.ListenerEntryStatus {
		return gatewayx.ListenerEntryStatus{
			Name:           e.Name,
			Port:           l.Port,
			SupportedKinds: e.SupportedKinds,
			AttachedRoutes: e.AttachedRoutes,
			Conditions:     e.Conditions,
		}
	}
}

func convertListenerSetStatusToStandardStatus(e gatewayx.ListenerEntryStatus) gateway.ListenerStatus {
	return gateway.ListenerStatus{
		Name:           e.Name,
		SupportedKinds: e.SupportedKinds,
		AttachedRoutes: e.AttachedRoutes,
		Conditions:     e.Conditions,
	}
}

func convertListenerSetToListener(l gatewayx.ListenerEntry) gateway.Listener {
	// For now, structs are identical enough Go can cast them. I doubt this will hold up forever, but we can adjust as needed.
	return gateway.Listener(l)
}
