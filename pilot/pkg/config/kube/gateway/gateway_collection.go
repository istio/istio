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

	"istio.io/api/annotation"
	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
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
	return config.NamespacedName(g.Config).String()
}

func (g ListenerSet) Equals(other ListenerSet) bool {
	return g.Config.Equals(other.Config) &&
		g.GatewayParent == other.GatewayParent &&
		g.Valid == other.Valid // TODO: ok to ignore parent/parentInfo?
}

func ListenerSetCollection(
	listenerSets krt.Collection[*gatewayv1.ListenerSet],
	gateways krt.Collection[*gatewayv1.Gateway],
	gatewayConflicts krt.Collection[gatewaycommon.GatewayListenerConflicts],
	gatewayClasses krt.Collection[gatewaycommon.GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants gatewaycommon.ReferenceGrants,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[gatewaycommon.GatewayContext]],
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus],
	krt.Collection[ListenerSet],
) {
	// Note: tagWatcher.IsMine() is intentionally not filtered at this config-emission layer. Filtering
	// here caused a temporary outage when a Gateway's or ListenerSet's istio.io/rev label was changed:
	// the prior owning control plane immediately dropped the resource and pushed empty xDS config to
	// pods still running on the old revision (see https://github.com/istio/istio/issues/59959).
	// Status writes are filtered to the owning revision via RegisterStatus in
	// pilot/pkg/status/collections.go, and Deployment management is filtered in deploymentcontroller.go,
	// so emitting config from non-owning revisions is safe and matches how core Istio CRDs behave.
	statusCol, gw := krt.NewStatusManyCollection(listenerSets,
		func(ctx krt.HandlerContext, obj *gatewayv1.ListenerSet) (*gatewayv1.ListenerSetStatus, []ListenerSet) {
			// We currently depend on service discovery information not know to krt; mark we depend on it.
			context := gatewayContext.Get(ctx).Load()
			if context == nil {
				return nil, nil
			}
			result := []ListenerSet{}
			ls := obj.Spec
			status := obj.Status.DeepCopy()

			p := ls.ParentRef
			if gatewaycommon.NormalizeReference(p.Group, p.Kind, gvk.KubernetesGateway) != gvk.KubernetesGateway {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			pns := ptr.OrDefault(p.Namespace, gatewayv1.Namespace(obj.Namespace))
			parentGwObj := ptr.Flatten(krt.FetchOne(ctx, gateways, krt.FilterKey(string(pns)+"/"+string(p.Name))))
			if parentGwObj == nil {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			class := gatewaycommon.FetchGatewayClass(ctx, gatewayClasses, parentGwObj.Spec.GatewayClassName)
			if class == nil {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			controllerName := class.Controller
			classInfo, f := gatewaycommon.ClassInfos[controllerName]
			if !f {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}
			if !classInfo.SupportsListenerSet {
				gatewaycommon.ReportUnsupportedListenerSet(class.Name, status, obj)
				return status, nil
			}

			if !gatewaycommon.NamespaceAcceptedByAllowListeners(obj.Namespace, parentGwObj, func(s string) *corev1.Namespace {
				return ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(s)))
			}) {
				gatewaycommon.ReportNotAllowedListenerSet(status, obj)
				return status, nil
			}

			gatewayServices, err := extractGatewayServices(domainSuffix, parentGwObj, classInfo)
			if len(gatewayServices) == 0 && err != nil {
				// Short circuit if it's a hard failure
				gatewaycommon.ReportListenerSetStatus(context, parentGwObj, obj, status, gatewayServices, nil, listenerSetParentErr(err), true)
				return status, nil
			}

			var conflicts map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason
			if gwConflict := krt.FetchOne(ctx, gatewayConflicts, krt.FilterKey(config.NamespacedName(parentGwObj).String())); gwConflict != nil {
				conflicts = gwConflict.ConflictsFor(obj)
			}

			servers := []*istio.Server{}
			validListeners := 0
			standardStatus := slices.Map(status.Listeners, gatewaycommon.ConvertListenerSetStatusToStandardStatus)
			for i, l := range ls.Listeners {
				port, portErr := gatewaycommon.ListenerEntryPortNumber(l)
				l.Port = port
				standardListener := gatewaycommon.ConvertListenerSetToListener(l)

				if reason, ok := conflicts[l.Name]; ok {
					standardStatus = gatewaycommon.ReportListenerConflict(i, standardListener, obj, standardStatus, reason)
					continue
				}

				server, updatedStatus, programmed := buildListener(ctx, configMaps, secrets, grants, namespaces,
					obj, standardStatus, parentGwObj.Spec, standardListener, i, controllerName, portErr)
				standardStatus = updatedStatus

				if server == nil {
					continue
				}

				if programmed {
					validListeners++
				}
				servers = append(servers, server)
				if controllerName == constants.ManagedGatewayMeshController {
					// Waypoint doesn't convert routes to VirtualServices.
					continue
				}
				if controllerName == constants.ManagedGatewayEastWestController && port == 15008 {
					// The HBONE listener on port 15008 does not use route-based VirtualService conversion.
					// Non-15008 listeners (e.g. TLS passthrough) are handled like regular gateway listeners below.
					continue
				}
				meta := parentMeta(obj, &l.Name)
				meta[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway
				meta[model.InternalGatewayServiceAnnotation] = strings.Join(gatewayServices, ",")
				meta[constants.InternalParentNamespace] = parentGwObj.Namespace

				// For unmanaged (manual deployment) parent Gateways, we have no idea what service accounts
				// the gateway workloads will use, so we must not enforce service account restrictions.
				// See: https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api/#manual-deployment
				serviceAccountName := ""
				if gatewaycommon.IsManaged(&parentGwObj.Spec) {
					serviceAccountName = model.GetOrDefault(
						parentGwObj.GetAnnotations()[annotation.GatewayServiceAccount.Name],
						gatewaycommon.GetDefaultName(parentGwObj.GetName(), &parentGwObj.Spec, classInfo.DisableNameSuffix),
					)
				}
				meta[constants.InternalServiceAccount] = serviceAccountName

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

				allowed, _ := gatewaycommon.GenerateSupportedKinds(standardListener)
				ref := parentKey{
					Kind:      gvk.ListenerSet,
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

			status.Listeners = slices.Map(standardStatus, gatewaycommon.ConvertStandardStatusToListenerSetStatus)
			gatewaycommon.ReportListenerSetStatus(context, parentGwObj, obj, status, gatewayServices, servers, listenerSetParentErr(err), validListeners > 0)
			return status, result
		}, opts.WithName("ListenerSets")...)

	return statusCol, gw
}

func listenerSetParentErr(err *ConfigError) *gatewaycommon.ListenerStatusConfigError {
	if err == nil {
		return nil
	}
	return &gatewaycommon.ListenerStatusConfigError{Reason: err.Reason, Message: err.Message}
}

func GatewayCollection(
	gateways krt.Collection[*gatewayv1.Gateway],
	listenerSets krt.Collection[ListenerSet],
	gatewayConflicts krt.Collection[gatewaycommon.GatewayListenerConflicts],
	gatewayClasses krt.Collection[gatewaycommon.GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants gatewaycommon.ReferenceGrants,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[gatewaycommon.GatewayContext]],
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gatewayv1.Gateway, gatewayv1.GatewayStatus],
	krt.Collection[Gateway],
) {
	listenerIndex := krt.NewIndex(listenerSets, "gatewayParent", func(o ListenerSet) []types.NamespacedName {
		return []types.NamespacedName{o.GatewayParent}
	})
	// Note: tagWatcher.IsMine() is intentionally not filtered at this config-emission layer. See the
	// comment in ListenerSetCollection above for the rationale.
	statusCol, gw := krt.NewStatusManyCollection(gateways, func(ctx krt.HandlerContext, obj *gatewayv1.Gateway) (*gatewayv1.GatewayStatus, []Gateway) {
		// We currently depend on service discovery information not known to krt; mark we depend on it.
		context := gatewayContext.Get(ctx).Load()
		if context == nil {
			return nil, nil
		}
		result := []Gateway{}
		kgw := obj.Spec
		status := obj.Status.DeepCopy()
		class := gatewaycommon.FetchGatewayClass(ctx, gatewayClasses, kgw.GatewayClassName)
		if class == nil {
			return nil, nil
		}
		controllerName := class.Controller
		classInfo, f := gatewaycommon.ClassInfos[controllerName]
		if !f {
			return nil, nil
		}
		if classInfo.DisableRouteGeneration {
			reportUnmanagedGatewayStatus(status, obj)
			// We found it, but don't want to handle this class
			return status, nil
		}
		servers := []*istio.Server{}

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, err := extractGatewayServices(domainSuffix, obj, classInfo)
		if len(gatewayServices) == 0 && err != nil {
			// Short circuit if its a hard failure
			backendTLSErr := validateBackendClientCertificateRef(ctx, obj, secrets, grants)
			reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, 0, err, backendTLSErr)
			return status, nil
		}

		if err == nil {
			err = validateParametersRef(ctx, obj, configMaps)
		}

		// See: https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api/#manual-deployment
		// If we set and address of type hostname, then we have no idea what service accounts the gateway workloads will use.
		// Thus, we don't enforce service account name restrictions (still look at namespaces though).
		serviceAccountName := ""
		if gatewaycommon.IsManaged(&obj.Spec) {
			serviceAccountName = model.GetOrDefault(
				obj.GetAnnotations()[annotation.GatewayServiceAccount.Name],
				gatewaycommon.GetDefaultName(obj.GetName(), &kgw, classInfo.DisableNameSuffix),
			)
		}

		var gwListenerConflicts map[gatewayv1.SectionName]gatewayv1.ListenerConditionReason
		if gwConflict := krt.FetchOne(ctx, gatewayConflicts, krt.FilterKey(config.NamespacedName(obj).String())); gwConflict != nil {
			gwListenerConflicts = gwConflict.ConflictsForGateway(obj)
		}

		for i, l := range kgw.Listeners {
			if reason, ok := gwListenerConflicts[l.Name]; ok {
				status.Listeners = gatewaycommon.ReportListenerConflict(i, l, obj, status.Listeners, reason)
				continue
			}

			server, updatedStatus, programmed := buildListener(ctx, configMaps, secrets, grants, namespaces, obj, status.Listeners, kgw, l, i, controllerName, nil)
			status.Listeners = updatedStatus

			if server == nil {
				continue
			}

			servers = append(servers, server)

			if controllerName == constants.ManagedGatewayMeshController {
				// Waypoint doesn't convert routes to VirtualServices.
				continue
			}
			if controllerName == constants.ManagedGatewayEastWestController && l.Port == 15008 {
				// The HBONE listener on port 15008 does not use route-based VirtualService conversion.
				// Non-15008 listeners (e.g. TLS passthrough) are handled like regular gateway listeners below.
				continue
			}
			meta := parentMeta(obj, &l.Name)
			meta[constants.InternalGatewaySemantics] = constants.GatewaySemanticsGateway
			meta[model.InternalGatewayServiceAnnotation] = strings.Join(gatewayServices, ",")

			meta[constants.InternalServiceAccount] = serviceAccountName

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

			allowed, _ := gatewaycommon.GenerateSupportedKinds(l)
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
		listenersFromSets := listenerIndex.Fetch(ctx, config.NamespacedName(obj))
		// When reporting gateway status, we need to count the actual ListenerSets attached to the gateway
		// and not the listeners.
		listenerSets := make(map[parentKey]bool)
		for _, ls := range listenersFromSets {
			if !ls.Valid {
				continue
			}
			listenerSets[ls.Parent] = true
			servers = append(servers, ls.Config.Spec.(*istio.Gateway).Servers...)
			result = append(result, Gateway{
				Config:     ls.Config,
				Parent:     ls.Parent,
				ParentInfo: ls.ParentInfo,
				Valid:      ls.Valid,
			})
		}

		backendTLSErr := validateBackendClientCertificateRef(ctx, obj, secrets, grants)
		reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, len(listenerSets), err, backendTLSErr)
		return status, result
	}, opts.WithName("KubernetesGateway")...)

	return statusCol, gw
}

// FinalGatewayStatusCollection finalizes a Gateway status. There is a circular logic between Gateways and Routes to determine
// the attachedRoute count, so we first build a partial Gateway status, then once routes are computed we finalize it with
// the attachedRoute count.
func FinalGatewayStatusCollection(
	gatewayStatuses krt.StatusCollection[*gatewayv1.Gateway, gatewayv1.GatewayStatus],
	routeAttachments krt.Collection[RouteAttachment],
	routeAttachmentsIndex krt.Index[types.NamespacedName, RouteAttachment],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gatewayv1.Gateway, gatewayv1.GatewayStatus] {
	return krt.NewCollection(
		gatewayStatuses,
		func(
			ctx krt.HandlerContext, i krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus],
		) *krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus] {
			tcpRoutes := routeAttachmentsIndex.Fetch(ctx, config.NamespacedName(i.Obj))
			counts := map[string]int32{}
			for _, r := range tcpRoutes {
				counts[r.ListenerName]++
			}
			status := i.Status.DeepCopy()
			for i, s := range status.Listeners {
				s.AttachedRoutes = counts[string(s.Name)]
				status.Listeners[i] = s
			}
			return &krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus]{
				Obj:    i.Obj,
				Status: *status,
			}
		}, opts.WithName("GatewayFinalStatus")...)
}

// FinalListenerSetStatusCollection finalizes a ListenerSet status similarly to how FinalGatewayStatusCollection does it for gateways.
func FinalListenerSetStatusCollection(
	listenerSetStatuses krt.StatusCollection[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus],
	routeAttachments krt.Collection[RouteAttachment],
	routeAttachmentsIndex krt.Index[types.NamespacedName, RouteAttachment],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gatewayv1.ListenerSet, gatewayv1.ListenerSetStatus] {
	return gatewaycommon.FinalListenerSetStatusCollection(
		listenerSetStatuses,
		func(ctx krt.HandlerContext, obj *gatewayv1.ListenerSet) []RouteAttachment {
			return routeAttachmentsIndex.Fetch(ctx, config.NamespacedName(obj))
		},
		func(r RouteAttachment) string { return r.ListenerName },
		opts,
	)
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
				AllowedKinds: []gatewayv1.RouteGroupKind{
					{Group: (*gatewayv1.Group)(ptr.Of(gvk.HTTPRoute.Group)), Kind: gatewayv1.Kind(gvk.HTTPRoute.Kind)},
					{Group: (*gatewayv1.Group)(ptr.Of(gvk.GRPCRoute.Group)), Kind: gatewayv1.Kind(gvk.GRPCRoute.Kind)},
					{Group: (*gatewayv1.Group)(ptr.Of(gvk.TCPRoute.Group)), Kind: gatewayv1.Kind(gvk.TCPRoute.Kind)},
					{Group: (*gatewayv1.Group)(ptr.Of(gvk.TLSRoute.Group)), Kind: gatewayv1.Kind(gvk.TLSRoute.Kind)},
				},
			},
		}
	}
	return slices.Map(p.gatewayIndex.Fetch(ctx, pk), func(gw Gateway) *parentInfo {
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

func validateParametersRef(ctx krt.HandlerContext, gw *gatewayv1.Gateway, configMaps krt.Collection[*corev1.ConfigMap]) *ConfigError {
	if gw.Spec.Infrastructure == nil || gw.Spec.Infrastructure.ParametersRef == nil {
		return nil
	}
	params := gw.Spec.Infrastructure.ParametersRef
	// Validate that the ParametersRef group/kind is of a ConfigMap.
	if string(params.Kind) != gvk.ConfigMap.Kind || string(params.Group) != gvk.ConfigMap.Group {
		return &ConfigError{
			Reason:  string(gatewayv1.GatewayReasonInvalidParameters),
			Message: fmt.Sprintf("Unsupported parametersRef group/kind %s/%s, only ConfigMap is supported", params.Group, params.Kind),
		}
	}
	// Validate ParametersRef exists.
	cm := ptr.Flatten(krt.FetchOne(ctx, configMaps, krt.FilterKey(gw.Namespace+"/"+params.Name)))
	if cm == nil {
		return &ConfigError{
			Reason:  string(gatewayv1.GatewayReasonInvalidParameters),
			Message: fmt.Sprintf("parametersRef ConfigMap %s/%s does not exist", gw.Namespace, params.Name),
		}
	}
	return nil
}
