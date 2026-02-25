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
	"bytes"
	"fmt"

	"github.com/agentgateway/agentgateway/go/api"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
)

// Used by agentgateway controller
// TLSInfo contains the TLS certificate and key for a gateway listener.
type TLSInfo struct {
	Cert   []byte
	CaCert []byte
	Key    []byte `json:"-"`
}

// Used by agentgateway controller
type GatewayListener struct {
	Name string
	// The Gateway this listener is bound to
	ParentGateway types.NamespacedName
	// The actual real parent (could be a ListenerSet)
	ParentObject AgwParentKey
	ParentInfo   AgwParentInfo
	TLSInfo      *TLSInfo
	Valid        bool
}

func (g GatewayListener) ResourceName() string {
	return g.Name
}

func (g GatewayListener) Equals(other GatewayListener) bool {
	if (g.TLSInfo != nil) != (other.TLSInfo != nil) {
		return false
	}
	if g.TLSInfo != nil {
		if !bytes.Equal(g.TLSInfo.Cert, other.TLSInfo.Cert) ||
			!bytes.Equal(g.TLSInfo.Key, other.TLSInfo.Key) ||
			!bytes.Equal(g.TLSInfo.CaCert, other.TLSInfo.CaCert) {
			return false
		}
	}
	return g.Valid == other.Valid && g.Name == other.Name && g.ParentGateway == other.ParentGateway && g.ParentObject == other.ParentObject && g.ParentInfo.Equals(other.ParentInfo)
}

func (g AgwParentInfo) Equals(other AgwParentInfo) bool {
	return g.ParentGateway == other.ParentGateway &&
		g.InternalName == other.InternalName &&
		g.OriginalHostname == other.OriginalHostname &&
		g.SectionName == other.SectionName &&
		g.Port == other.Port &&
		g.Protocol == other.Protocol &&
		g.TLSPassthrough == other.TLSPassthrough &&
		slices.EqualFunc(g.AllowedKinds, other.AllowedKinds, func(a, b gatewayv1.RouteGroupKind) bool {
			return a.Kind == b.Kind && ptr.Equal(a.Group, b.Group)
		}) &&
		slices.Equal(g.Hostnames, other.Hostnames)
}

// Borrowed from kgateway
type ListenerSet struct {
	Name string `json:"name"`
	// +krtEqualsTodo include parent gateway identity in equality check
	Parent types.NamespacedName `json:"parent"`
	// +krtEqualsTodo ensure parent metadata differences trigger equality
	ParentInfo    AgwParentInfo        `json:"parentInfo"`
	TLSInfo       *TLSInfo             `json:"tlsInfo"`
	GatewayParent types.NamespacedName `json:"gatewayParent"`
	Valid         bool                 `json:"valid"`
}

func (g ListenerSet) ResourceName() string {
	return g.Name
}

func (g ListenerSet) Equals(other ListenerSet) bool {
	if (g.TLSInfo != nil) != (other.TLSInfo != nil) {
		return false
	}
	if g.TLSInfo != nil {
		if !bytes.Equal(g.TLSInfo.Cert, other.TLSInfo.Cert) && !bytes.Equal(g.TLSInfo.Key, other.TLSInfo.Key) {
			return false
		}
	}
	return g.Name == other.Name &&
		g.GatewayParent == other.GatewayParent &&
		g.Valid == other.Valid
}

func ListenerSetCollection(
	listenerSets krt.Collection[*gatewayx.XListenerSet],
	gateways krt.Collection[*gatewayv1.Gateway],
	gatewayClasses krt.Collection[GatewayClass],
	namespaces krt.Collection[*corev1.Namespace],
	grants gatewaycommon.ReferenceGrants,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
	domainSuffix string,
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[gatewaycommon.GatewayContext]],
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

			class := FetchClass(ctx, gatewayClasses, parentGwObj.Spec.GatewayClassName)
			if class == nil {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			controllerName := class.Controller
			classInfo, f := gatewaycommon.AgentgatewayClassInfos[controllerName]
			if !f {
				// Cannot report status since we don't know if it is for us
				return nil, nil
			}

			if !classInfo.SupportsListenerSet {
				reportUnsupportedListenerSet(class.Name, status, obj)
				return status, nil
			}

			if !gatewaycommon.NamespaceAcceptedByAllowListeners(obj.Namespace, parentGwObj, func(s string) *corev1.Namespace {
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
				standardListener := gatewaycommon.ConvertListenerSetToListener(l)
				originalStatus := slices.Map(status.Listeners, convertListenerSetStatusToStandardStatus)
				server, tlsInfo, updatedStatus, programmed := buildListener(ctx, secrets, configMaps, grants, namespaces,
					obj, originalStatus, parentGwObj.Spec, standardListener, i, controllerName, portErr)
				status.Listeners = slices.Map(updatedStatus, convertStandardStatusToListenerSetStatus(l))

				servers = append(servers, server)

				if controllerName == constants.ManagedGatewayMeshController || controllerName == constants.ManagedGatewayEastWestController {
					// Waypoint doesn't actually convert the routes to VirtualServices
					continue
				}
				name := InternalGatewayName(obj.Namespace, obj.Name, string(l.Name))

				allowed, _ := generateSupportedKinds(standardListener)
				pri := AgwParentInfo{
					ParentGateway:    config.NamespacedName(parentGwObj),
					InternalName:     obj.Namespace + "/" + name,
					AllowedKinds:     allowed,
					Hostnames:        server.Hosts,
					OriginalHostname: string(ptr.OrEmpty(l.Hostname)),
					SectionName:      l.Name,
					Port:             l.Port,
					Protocol:         l.Protocol,
					TLSPassthrough:   l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == gatewayv1.TLSModePassthrough,
				}

				res := ListenerSet{
					Name:          name,
					Valid:         programmed,
					TLSInfo:       tlsInfo,
					Parent:        config.NamespacedName(obj),
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

func reportListenerSetStatus(
	r *gatewaycommon.GatewayContext,
	parentGwObj *gatewayv1.Gateway,
	obj *gatewayx.XListenerSet,
	gs *gatewayx.ListenerSetStatus,
	gatewayServices []string,
	servers []*istio.Server,
	cond *condition,
) {
	internal, _, _, _, warnings, allUsable := r.ResolveGatewayInstances(parentGwObj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason:  string(gatewayv1.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason:  string(gatewayv1.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if cond != nil && cond.error != nil {
		cond.error.Message = "Parent not accepted: " + cond.error.Message
		gatewayConditions[string(gatewayv1.GatewayConditionAccepted)].error = cond.error
	}

	setProgrammedCondition(gatewayConditions, internal, gatewayServices, warnings, allUsable)

	gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
}

func GatewayCollection(
	gateways krt.Collection[*gatewayv1.Gateway],
	listenerSets krt.Collection[ListenerSet],
	gatewayClasses krt.Collection[GatewayClass],
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
	krt.Collection[*GatewayListener],
) {
	listenerIndex := krt.NewIndex(listenerSets, "gatewayParent", func(o ListenerSet) []types.NamespacedName {
		return []types.NamespacedName{o.GatewayParent}
	})
	gwstatus, gw := krt.NewStatusManyCollection(gateways, func(ctx krt.HandlerContext, obj *gatewayv1.Gateway) (*gatewayv1.GatewayStatus, []*GatewayListener) {
		context := gatewayContext.Get(ctx).Load()
		if context == nil {
			return nil, nil
		}
		if !tagWatcher.Get(ctx).IsMine(obj.ObjectMeta) {
			return nil, nil
		}
		result := []*GatewayListener{}
		kgw := obj.Spec
		status := obj.Status.DeepCopy()

		class := FetchClass(ctx, gatewayClasses, kgw.GatewayClassName)
		if class == nil {
			return nil, nil
		}

		controllerName := class.Controller
		classInfo, f := gatewaycommon.AgentgatewayClassInfos[controllerName]
		if !f {
			return nil, nil
		}
		if classInfo.DisableRouteGeneration {
			// TODO(jaellio): Applicable for agent gateway?
			// reportUnmanagedGatewayStatus(status, obj)
			// We found it, but don't want to handle this class
			return status, nil
		}
		servers := []*istio.Server{}

		logger.Debugf("translating Gateway gw_name: %s, resource_version: %s", obj.GetName(), obj.GetResourceVersion())

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, err := extractGatewayServices(domainSuffix, obj, classInfo)
		if len(gatewayServices) == 0 && err != nil {
			// Short circuit if its a hard failure
			logger.Errorf("failed to translate gwv1", "name", obj.GetName(), "namespace", obj.GetNamespace(), "err", err.message)
			reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, 0, err.error)
			return status, nil
		}
		var gatewayErr *ConfigError
		if err != nil {
			gatewayErr = err.error
		}

		for i, l := range kgw.Listeners {
			// Attached Routes count starts at 0 and gets updated later in the status syncer
			// when the real count is available after route processing
			server, tlsInfo, updatedStatus, programmed := buildListener(ctx, secrets, configMaps, grants, namespaces, obj, status.Listeners, kgw, l, i, controllerName, nil)
			status.Listeners = updatedStatus

			servers = append(servers, server)

			// Generate supported kinds for the listener
			allowed, _ := generateSupportedKinds(l)

			name := InternalGatewayName(obj.Namespace, obj.Name, string(l.Name))
			pri := AgwParentInfo{
				ParentGateway:          config.NamespacedName(obj),
				ParentGatewayClassName: string(obj.Spec.GatewayClassName),
				InternalName:           InternalGatewayName(obj.Namespace, name, ""),
				AllowedKinds:           allowed,
				Hostnames:              server.Hosts,
				OriginalHostname:       string(ptr.OrEmpty(l.Hostname)),
				SectionName:            l.Name,
				Port:                   l.Port,
				Protocol:               l.Protocol,
				TLSPassthrough:         l.TLS != nil && l.TLS.Mode != nil && *l.TLS.Mode == gatewayv1.TLSModePassthrough,
			}

			res := &GatewayListener{
				Name:          name,
				Valid:         programmed,
				TLSInfo:       tlsInfo,
				ParentGateway: config.NamespacedName(obj),
				ParentObject: AgwParentKey{
					Kind:      gvk.KubernetesGateway.Kubernetes(),
					Name:      obj.Name,
					Namespace: obj.Namespace,
				},
				ParentInfo: pri,
			}
			result = append(result, res)
		}
		listenersFromSets := krt.Fetch(ctx, listenerSets, krt.FilterIndex(listenerIndex, config.NamespacedName(obj)))
		for _, ls := range listenersFromSets {
			result = append(result, &GatewayListener{
				Name:          ls.Name,
				ParentGateway: config.NamespacedName(obj),
				ParentObject: AgwParentKey{
					Kind:      gvk.XListenerSet.Kubernetes(),
					Name:      ls.Parent.Name,
					Namespace: ls.Parent.Namespace,
				},
				TLSInfo:    ls.TLSInfo,
				ParentInfo: ls.ParentInfo,
				Valid:      ls.Valid,
			})
		}

		reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, len(listenersFromSets), gatewayErr)
		return status, result
	}, opts.WithName("KubernetesGateway")...)

	return gwstatus, gw
}

// InternalGatewayName returns the name of the internal Gateway corresponding to the
// specified gwv1-api gwv1 and listener. If the listener is not specified, returns internal name without listener.
// Format: gwNs/gwName.listener
func InternalGatewayName(gwNamespace, gwName, lName string) string {
	if lName == "" {
		return fmt.Sprintf("%s/%s", gwNamespace, gwName)
	}
	return fmt.Sprintf("%s/%s.%s", gwNamespace, gwName, lName)
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
			return &krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus]{
				Obj:    i.Obj,
				Status: *status,
			}
		}, opts.WithName("GatewayFinalStatus")...)
}

// RouteParents holds information about things routes can reference as parents.
type RouteParents struct {
	gateways     krt.Collection[*GatewayListener]
	gatewayIndex krt.Index[AgwParentKey, *GatewayListener]
}

func (p RouteParents) fetch(ctx krt.HandlerContext, pk AgwParentKey) []*AgwParentInfo {
	return slices.Map(krt.Fetch(ctx, p.gateways, krt.FilterIndex(p.gatewayIndex, pk)), func(gw *GatewayListener) *AgwParentInfo {
		return &gw.ParentInfo
	})
}

func BuildRouteParents(
	gateways krt.Collection[*GatewayListener],
) RouteParents {
	idx := krt.NewIndex(gateways, "parent", func(o *GatewayListener) []AgwParentKey {
		return []AgwParentKey{o.ParentObject}
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

func convertStandardStatusToListenerSetStatus(l gatewayx.ListenerEntry) func(e gatewayv1.ListenerStatus) gatewayx.ListenerEntryStatus {
	return func(e gatewayv1.ListenerStatus) gatewayx.ListenerEntryStatus {
		return gatewayx.ListenerEntryStatus{
			Name:           e.Name,
			Port:           l.Port,
			SupportedKinds: e.SupportedKinds,
			AttachedRoutes: e.AttachedRoutes,
			Conditions:     e.Conditions,
		}
	}
}

func convertListenerSetStatusToStandardStatus(e gatewayx.ListenerEntryStatus) gatewayv1.ListenerStatus {
	return gatewayv1.ListenerStatus{
		Name:           e.Name,
		SupportedKinds: e.SupportedKinds,
		AttachedRoutes: e.AttachedRoutes,
		Conditions:     e.Conditions,
	}
}

// AgwBind is a wrapper type that contains the bind on the gateway, as well as the status for the bind.
type AgwBind struct {
	*api.Bind
}

func (g AgwBind) ResourceName() string {
	return g.Key
}

func (g AgwBind) Equals(other AgwBind) bool {
	return protoconv.Equals(g, other)
}

// AgwListener is a wrapper type that contains the listener on the gateway, as well as the status for the listener.
type AgwListener struct {
	*api.Listener
}

func (g AgwListener) ResourceName() string {
	return g.Key
}

func (g AgwListener) Equals(other AgwListener) bool {
	return protoconv.Equals(g, other)
}

// AgwRoute is a wrapper type that contains the route on the gateway, as well as the status for the route.
type AgwRoute struct {
	*api.Route
}

func (g AgwRoute) ResourceName() string {
	return g.Key
}

func (g AgwRoute) Equals(other AgwRoute) bool {
	return protoconv.Equals(g, other)
}

// AgwTCPRoute is a wrapper type that contains the tcp route on the gateway, as well as the status for the tcp route.
type AgwTCPRoute struct {
	*api.TCPRoute
}

func (g AgwTCPRoute) ResourceName() string {
	return g.Key
}

func (g AgwTCPRoute) Equals(other AgwTCPRoute) bool {
	return protoconv.Equals(g, other)
}
