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
	"strconv"
	"strings"
	"sync"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	istio "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	kubeconfig "istio.io/istio/pkg/config/gateway/kube"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("gateway", "gateway-api controller")

var errUnsupportedOp = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")

// Controller defines the controller for the gateway-api. The controller acts a bit different from most.
// Rather than watching the CRs directly, we depend on the existing model.ConfigStoreController which
// already watches all CRs. When there are updates, a new PushContext will be computed, which will eventually
// call Controller.Reconcile(). Once this happens, we will inspect the current state of the world, and transform
// gateway-api types into Istio types (Gateway/VirtualService). Future calls to Get/List will return these
// Istio types. These are not stored in the cluster at all, and are purely internal; they can be seen on /debug/configz.
// During Reconcile(), the status on all gateway-api types is also tracked. Once completed, if the status
// has changed at all, it is queued to asynchronously update the status of the object in Kubernetes.
type Controller struct {
	// client for accessing Kubernetes
	client kube.Client

	// Gateway-api types reference namespace labels directly, so we need access to these
	namespaceHandler model.EventHandler

	// Gateway-api types reference secrets directly, so we need access to these
	secretHandler model.EventHandler

	// the cluster where the gateway-api controller runs
	cluster cluster.ID
	// domain stores the cluster domain, typically cluster.local
	domain string

	// state is our computed Istio resources. Access is guarded by stateMu. This is updated from Reconcile().
	state   IstioResources
	stateMu sync.RWMutex

	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	statusWriter *StatusWriter

	tagWatcher revisions.TagWatcher

	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool
	outputs    Outputs

	gatewayContext        *atomic.Pointer[GatewayContext]
	gatewayContextTrigger *krt.RecomputeTrigger
	stop                  chan struct{}
}

type GatewayClass struct {
	Name       string
	Controller gateway.GatewayController
}

func (g GatewayClass) ResourceName() string {
	return g.Name
}

func GatewayClassesCollection(
	GatewayClasses krt.Collection[*gateway.GatewayClass],
	opts krt.OptionsBuilder,
) (
	krt.Collection[krt.ObjectWithStatus[*gateway.GatewayClass, *gateway.GatewayClassStatus]],
	krt.Collection[GatewayClass],
) {
	return krt.NewStatusCollection(GatewayClasses, func(ctx krt.HandlerContext, obj *gateway.GatewayClass) (**gateway.GatewayClassStatus, *GatewayClass) {
		log.Errorf("howardjohn: have GC %+v", obj)
		_, known := classInfos[obj.Spec.ControllerName]
		if !known {
			return nil, nil
		}
		status := GetClassStatus(&obj.Status, obj.Generation)
		return ptr.Of(&status), &GatewayClass{
			Name:       obj.Name,
			Controller: obj.Spec.ControllerName,
		}
	}, opts.WithName("GatewayClasses")...)
}

type ReferencePair struct {
	To, From Reference
}

func (r ReferencePair) String() string {
	return fmt.Sprintf("%s->%s", r.To, r.From)
}

type ReferenceGrants struct {
	collection krt.Collection[ReferenceGrant]
	index      krt.Index[ReferencePair, ReferenceGrant]
}

func BuildReferenceGrants(collection krt.Collection[ReferenceGrant]) ReferenceGrants {
	idx := krt.NewIndex(collection, func(o ReferenceGrant) []ReferencePair {
		return []ReferencePair{{
			To:   o.To,
			From: o.From,
		}}
	})
	return ReferenceGrants{
		collection: collection,
		index:      idx,
	}
}

func (refs ReferenceGrants) SecretAllowed(ctx krt.HandlerContext, resourceName string, namespace string) bool {
	p, err := creds.ParseResourceName(resourceName, "", "", "")
	if err != nil {
		log.Warnf("failed to parse resource name %q: %v", resourceName, err)
		return false
	}
	from := Reference{Kind: gvk.KubernetesGateway, Namespace: gateway.Namespace(namespace)}
	to := Reference{Kind: gvk.Secret, Namespace: gateway.Namespace(p.Namespace)}
	pair := ReferencePair{From: from, To: to}
	grants := krt.Fetch(ctx, refs.collection, krt.FilterIndex(refs.index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == p.Name {
			return true
		}
	}
	return false
}

func (refs ReferenceGrants) BackendAllowed(ctx krt.HandlerContext,
	k config.GroupVersionKind,
	backendName gateway.ObjectName,
	backendNamespace gateway.Namespace,
	routeNamespace string,
) bool {
	from := Reference{Kind: k, Namespace: gateway.Namespace(routeNamespace)}
	to := Reference{Kind: gvk.Service, Namespace: backendNamespace}
	pair := ReferencePair{From: from, To: to}
	grants := krt.Fetch(ctx, refs.collection, krt.FilterIndex(refs.index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == string(backendName) {
			return true
		}
	}
	return false
}

type ReferenceGrant struct {
	Source      types.NamespacedName
	From        Reference
	To          Reference
	AllowAll    bool
	AllowedName string
}

func (g ReferenceGrant) ResourceName() string {
	return g.Source.String() + "/" + g.From.String() + "/" + g.To.String()
}

type RouteContext struct {
	Krt     krt.HandlerContext
	Grants  ReferenceGrants
	Parents Parents
	Domain  string
}

func ReferenceGrantsCollection(ReferenceGrants krt.Collection[*gateway.ReferenceGrant], opts krt.OptionsBuilder) krt.Collection[ReferenceGrant] {
	return krt.NewManyCollection(ReferenceGrants, func(ctx krt.HandlerContext, obj *gateway.ReferenceGrant) []ReferenceGrant {
		rp := obj.Spec
		results := make([]ReferenceGrant, 0, len(rp.From)*len(rp.To))
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
					Namespace: gateway.Namespace(obj.Namespace),
				}
				if to.Group == "" && string(to.Kind) == gvk.Secret.Kind {
					toKey.Kind = gvk.Secret
				} else if to.Group == "" && string(to.Kind) == gvk.Service.Kind {
					toKey.Kind = gvk.Service
				} else {
					// Not supported type. Not an error; may be for another controller
					continue
				}
				rg := ReferenceGrant{
					Source:      config.NamespacedName(obj),
					From:        fromKey,
					To:          toKey,
					AllowAll:    false,
					AllowedName: "",
				}
				if to.Name != nil {
					rg.AllowedName = string(*to.Name)
				} else {
					rg.AllowAll = true
				}
				results = append(results, rg)
			}
		}
		return results
	}, opts.WithName("ReferenceGrants")...)
}

type Gateway struct {
	config.Config
	Valid      bool // DO NOT USE if not valid
	parent     parentKey
	parentInfo parentInfo
}

func (g Gateway) ResourceName() string {
	return config.NamespacedName(g.Config).Name
}

func GatewayCollection(
	Gateways krt.Collection[*gateway.Gateway],
	GatewayClasses krt.Collection[GatewayClass],
	Namespaces krt.Collection[*corev1.Namespace],
	grants ReferenceGrants,
	DomainSuffix string,
	UnstableContext *atomic.Pointer[GatewayContext],
	UnstableContextTrigger *krt.RecomputeTrigger,
	opts krt.OptionsBuilder,
) (krt.Collection[krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus]], krt.Collection[Gateway]) {
	statusCol, gw := krt.NewStatusManyCollection(Gateways, func(ctx krt.HandlerContext, obj *gateway.Gateway) (*gateway.GatewayStatus, []Gateway) {
		UnstableContextTrigger.MarkDependant(ctx)
		context := UnstableContext.Load()
		if context == nil {
			return nil, nil
		}
		result := []Gateway{}
		kgw := obj.Spec
		status := kstatus.WrapT(&obj.Status)
		class := krt.FetchOne(ctx, GatewayClasses, krt.FilterKey(string(kgw.GatewayClassName)))
		if class == nil {
			// No gateway class found, this may be meant for another controller; should be skipped.
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
			return status.Status, nil
		}
		servers := []*istio.Server{}

		// Extract the addresses. A gateway will bind to a specific Service
		gatewayServices, err := extractGatewayServices(DomainSuffix, obj, classInfo)
		if len(gatewayServices) == 0 && err != nil {
			// Short circuit if its a hard failure
			reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, err)
			return status.Status, nil
		}

		for i, l := range kgw.Listeners {
			server, programmed := buildListener(ctx, grants, Namespaces, obj, status, l, i, controllerName)

			servers = append(servers, server)
			if controllerName == constants.ManagedGatewayMeshController {
				// Waypoint doesn't actually convert the routes to VirtualServices
				continue
			}
			meta := parentMeta2(obj, &l.Name)
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
					Domain:            DomainSuffix,
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
			pri.ReportAttachedRoutes = func() {
				// reportListenerAttachedRoutes(i, obj, pri.AttachedRoutes)
			}

			res := Gateway{
				Config:     gatewayConfig,
				Valid:      programmed,
				parent:     ref,
				parentInfo: pri,
			}
			result = append(result, res)
		}

		reportGatewayStatus(context, obj, status, classInfo, gatewayServices, servers, err)
		return status.Status, result
	}, opts.WithName("KubernetesGateway")...)

	return statusCol, gw
}

func registerStatus[I controllers.Object, IS any](statusCol krt.Collection[krt.ObjectWithStatus[I, IS]], statusWriter *StatusWriter) krt.Syncer {
	mu := sync.Mutex{}
	resync := func() {
		mu.Lock()
		defer mu.Unlock()
		items := statusCol.List()
		log.Errorf("howardjohn: resync %v items %v", ptr.TypeName[IS](), len(items))
		for _, l := range items {
			EnqueueStatus2(statusWriter, l.Obj, &l.Status)
		}
	}
	statusWriter.resyncers = append(statusWriter.resyncers, resync)
	return statusCol.Register(func(o krt.Event[krt.ObjectWithStatus[I, IS]]) {
		mu.Lock()
		defer mu.Unlock()
		l := o.Latest()
		log.Errorf("howardjohn: GOT STATUS OBJ")
		EnqueueStatus2(statusWriter, l.Obj, &l.Status)
	})
}

type Parents struct {
	gateways     krt.Collection[Gateway]
	gatewayIndex krt.Index[parentKey, Gateway]
}

func (p Parents) Fetch(ctx krt.HandlerContext, pk parentKey) []*parentInfo {
	return slices.Map(krt.Fetch(ctx, p.gateways, krt.FilterIndex(p.gatewayIndex, pk)), func(gw Gateway) *parentInfo {
		return &gw.parentInfo
	})
}

type ParentInfo struct {
	Key  parentKey
	Info parentInfo
}

func (pi ParentInfo) ResourceName() string {
	return pi.Key.Name // TODO!!!! more infoi and section name
}

func BuildParents(
	Gateways krt.Collection[Gateway],
) Parents {
	idx := krt.NewIndex(Gateways, func(o Gateway) []parentKey {
		return []parentKey{o.parent}
	})
	return Parents{
		gateways:     Gateways,
		gatewayIndex: idx,
	}
}

type RouteResult[I, IStatus any] struct {
	VirtualServices  krt.Collection[config.Config]
	RouteAttachments krt.Collection[RouteAttachment]
	Status           krt.Collection[krt.ObjectWithStatus[I, IStatus]]
}

func TLSRouteCollection(
	TLSRoutes krt.Collection[*gatewayalpha.TLSRoute],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	Parents Parents,
	grants ReferenceGrants,
	DomainSuffix string,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayalpha.TLSRoute, *gatewayalpha.TLSRouteStatus] {
	routeCount := routeAttachmentCollection(Parents, TLSRoutes, gvk.TLSRoute, opts)
	status, virtualServices := krt.NewStatusManyCollection(TLSRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TLSRoute) (**gatewayalpha.TLSRouteStatus, []config.Config) {
		status := obj.Status.DeepCopy()
		ctx := RouteContext{
			Krt:     krtctx,
			Grants:  grants,
			Parents: Parents,
			Domain:  DomainSuffix,
		}
		route := obj.Spec
		parentRefs := extractParentReferenceInfo2(ctx.Krt, Parents, route.ParentRefs, nil, gvk.TLSRoute, obj.Namespace)

		log.Errorf("howardjohn: compute for %T %v", obj, obj.GroupVersionKind())

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

		vs := []config.Config{}
		for _, parent := range filteredReferences(parentRefs) {
			routes := gwResult.routes
			vsHosts := []string{"*"}
			if parent.IsMesh() {
				routes = meshResult.routes
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, DomainSuffix)}
				}
				routes = augmentTLSPortMatch(routes, parent.OriginalReference.Port, vsHosts)
			}
			for i, host := range vsHosts {
				name := fmt.Sprintf("%s-tls-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
				filteredRoutes := routes
				if parent.IsMesh() {
					filteredRoutes = compatibleRoutesForHost(routes, host)
				}
				// Create one VS per hostname with a single hostname.
				// This ensures we can treat each hostname independently, as the spec requires
				vs = append(vs, config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta2(obj),
						Namespace:         obj.Namespace,
						Domain:            ctx.Domain,
					},
					Spec: &istio.VirtualService{
						Hosts:    []string{host},
						Gateways: []string{parent.InternalName},
						Tls:      filteredRoutes,
					},
				})
			}
		}
		return &status, vs
	}, opts.WithName("TLSRoute")...)
	return RouteResult[*gatewayalpha.TLSRoute, *gatewayalpha.TLSRouteStatus]{
		VirtualServices:  virtualServices,
		RouteAttachments: routeCount,
		Status:           status,
	}
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

func routeAttachmentCollection[T controllers.Object](
	Parents Parents,
	col krt.Collection[T],
	kind config.GroupVersionKind,
	opts krt.OptionsBuilder,
) krt.Collection[RouteAttachment] {
	return krt.NewManyCollection(col, func(krtctx krt.HandlerContext, obj T) []RouteAttachment {
		from := TypedResource{
			Kind: kind,
			Name: config.NamespacedName(obj),
		}
		log.Errorf("howardjohn: EXTRACTS")
		parents, hostnames := GetCommonRouteInfo(obj)
		parentRefs := extractParentReferenceInfo2(krtctx, Parents, parents, hostnames, kind, obj.GetNamespace())
		log.Errorf("howardjohn: /EXTRACTS")
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

func TCPRouteCollection(
	TCPRoutes krt.Collection[*gatewayalpha.TCPRoute],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	Parents Parents,
	grants ReferenceGrants,
	DomainSuffix string,
	opts krt.OptionsBuilder,
) RouteResult[*gatewayalpha.TCPRoute, *gatewayalpha.TCPRouteStatus] {
	routeCount := routeAttachmentCollection(Parents, TCPRoutes, gvk.TCPRoute, opts)
	status, virtualServices := krt.NewStatusManyCollection(TCPRoutes, func(krtctx krt.HandlerContext, obj *gatewayalpha.TCPRoute) (**gatewayalpha.TCPRouteStatus, []config.Config) {
		status := obj.Status.DeepCopy()
		ctx := RouteContext{
			Krt:     krtctx,
			Grants:  grants,
			Parents: Parents,
			Domain:  DomainSuffix,
		}
		route := obj.Spec
		log.Errorf("howardjohn: extract for %v", config.NamespacedName(obj))
		parentRefs := extractParentReferenceInfo2(ctx.Krt, Parents, route.ParentRefs, nil, gvk.TCPRoute, obj.Namespace)
		log.Errorf("howardjohn: done extract")
		for _, p := range parentRefs {
			log.Errorf("howardjohn: got p %+v", p)
		}
		log.Errorf("howardjohn: compute for %T %v", obj, obj.GroupVersionKind())

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

		vs := []config.Config{}
		for _, parent := range filteredReferences(parentRefs) {
			routes := gwResult.routes
			vsHosts := []string{"*"}
			if parent.IsMesh() {
				routes = meshResult.routes
				if parent.OriginalReference.Port != nil {
					routes = augmentTCPPortMatch(routes, *parent.OriginalReference.Port)
				}
				ref := types.NamespacedName{
					Namespace: string(ptr.OrDefault(parent.OriginalReference.Namespace, gateway.Namespace(obj.Namespace))),
					Name:      string(parent.OriginalReference.Name),
				}
				if parent.InternalKind == gvk.ServiceEntry {
					ses := ptr.Flatten(krt.FetchOne(ctx.Krt, ServiceEntries, krt.FilterKey(ref.String())))
					if ses != nil {
						vsHosts = ses.Spec.Hosts
					} else {
						// TODO: report an error
						vsHosts = []string{}
					}
				} else {
					vsHosts = []string{fmt.Sprintf("%s.%s.svc.%s", ref.Name, ref.Namespace, DomainSuffix)}
				}
			}
			for i, host := range vsHosts {
				name := fmt.Sprintf("%s-tcp-%d-%s", obj.Name, i, constants.KubernetesGatewayName)
				// Create one VS per hostname with a single hostname.
				// This ensures we can treat each hostname independently, as the spec requires
				vs = append(vs, config.Config{
					Meta: config.Meta{
						CreationTimestamp: obj.CreationTimestamp.Time,
						GroupVersionKind:  gvk.VirtualService,
						Name:              name,
						Annotations:       routeMeta2(obj),
						Namespace:         obj.Namespace,
						Domain:            DomainSuffix,
					},
					Spec: &istio.VirtualService{
						// We can use wildcard here since each listener can have at most one route bound to it, so we have
						// a single VS per Gateway.
						Hosts:    []string{host},
						Gateways: []string{parent.InternalName},
						Tcp:      routes,
					},
				})
			}
		}
		return &status, vs
	}, opts.WithName("TCPRoute")...)

	return RouteResult[*gatewayalpha.TCPRoute, *gatewayalpha.TCPRouteStatus]{
		VirtualServices:  virtualServices,
		RouteAttachments: routeCount,
		Status:           status,
	}
}

type Outputs struct {
	Gateways        krt.Collection[Gateway]
	VirtualServices krt.Collection[config.Config]
}

type Inputs struct {
	Namespaces      krt.Collection[*corev1.Namespace]
	GatewayClasses  krt.Collection[*gateway.GatewayClass]
	Gateways        krt.Collection[*gateway.Gateway]
	HTTPRoutes      krt.Collection[*gateway.HTTPRoute]
	GRPCRoutes      krt.Collection[*gatewayv1.GRPCRoute]
	TCPRoutes       krt.Collection[*gatewayalpha.TCPRoute]
	TLSRoutes       krt.Collection[*gatewayalpha.TLSRoute]
	ReferenceGrants krt.Collection[*gateway.ReferenceGrant]
	ServiceEntries  krt.Collection[*networkingclient.ServiceEntry]
}

var _ model.GatewayController = &Controller{}

func NewController(
	kc kube.Client,
	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool,
	options controller.Options,
) *Controller {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "gateway", options.KrtDebugger)

	statusWriter := &StatusWriter{statusController: atomic.NewPointer[status.Queue](nil)}
	gatewayController := &Controller{
		client:                kc,
		cluster:               options.ClusterID,
		domain:                options.DomainSuffix,
		tagWatcher:            revisions.NewTagWatcher(kc, options.Revision),
		statusWriter:          statusWriter,
		waitForCRD:            waitForCRD,
		gatewayContext:        atomic.NewPointer[GatewayContext](nil),
		gatewayContextTrigger: krt.NewRecomputeTrigger(false, opts.WithName("gatewayContextTrigger")...),
		stop:                  stop,
	}

	inputs := Inputs{
		Namespaces:      krt.NewInformer[*corev1.Namespace](kc, opts.WithName("Namespaces")...),
		GatewayClasses:  buildClient[*gateway.GatewayClass](kc, gvr.GatewayClass, opts, "GatewayClasses"),
		Gateways:        buildClient[*gateway.Gateway](kc, gvr.KubernetesGateway, opts, "Gateways"),
		HTTPRoutes:      buildClient[*gateway.HTTPRoute](kc, gvr.HTTPRoute, opts, "HTTPRoutes"),
		GRPCRoutes:      buildClient[*gatewayv1.GRPCRoute](kc, gvr.GRPCRoute, opts, "GRPCRoutes"),
		TCPRoutes:       buildClient[*gatewayalpha.TCPRoute](kc, gvr.TCPRoute, opts, "TCPRoutes"),
		TLSRoutes:       buildClient[*gatewayalpha.TLSRoute](kc, gvr.TLSRoute, opts, "TLSRoutes"),
		ReferenceGrants: buildClient[*gateway.ReferenceGrant](kc, gvr.ReferenceGrant, opts, "ReferenceGrants"),
		ServiceEntries:  buildClient[*networkingclient.ServiceEntry](kc, gvr.ServiceEntry, opts, "ServiceEntries"),
	}

	GatewayClassStatus, GatewayClasses := GatewayClassesCollection(inputs.GatewayClasses, opts)
	registerStatus(GatewayClassStatus, statusWriter)

	ReferenceGrants := BuildReferenceGrants(ReferenceGrantsCollection(inputs.ReferenceGrants, opts))

	// GatewaysStatus cannot is not fully complete until its join with route attachments to report attachedRoutes.
	// Do not register yet.
	GatewaysStatus, Gateways := GatewayCollection(
		inputs.Gateways,
		GatewayClasses,
		inputs.Namespaces,
		ReferenceGrants,
		options.DomainSuffix,
		gatewayController.gatewayContext,
		gatewayController.gatewayContextTrigger,
		opts,
	)

	// TODO add mesh
	Parents := BuildParents(Gateways)

	tcpRoutes := TCPRouteCollection(
		inputs.TCPRoutes,
		inputs.ServiceEntries,
		Parents,
		ReferenceGrants,
		options.DomainSuffix,
		opts,
	)
	registerStatus(tcpRoutes.Status, statusWriter)
	tlsRoutes := TLSRouteCollection(
		inputs.TLSRoutes,
		inputs.ServiceEntries,
		Parents,
		ReferenceGrants,
		options.DomainSuffix,
		opts,
	)
	registerStatus(tlsRoutes.Status, statusWriter)

	RouteAttachments := krt.JoinCollection([]krt.Collection[RouteAttachment]{
		tcpRoutes.RouteAttachments,
		tlsRoutes.RouteAttachments,
	})
	RouteAttachmentsIndex := krt.NewIndex(RouteAttachments, func(o RouteAttachment) []types.NamespacedName {
		return []types.NamespacedName{o.To}
	})

	GatewayFinalStatus := krt.NewCollection(GatewaysStatus, func(ctx krt.HandlerContext, i krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus]) *krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus] {
		tcpRoutes := krt.Fetch(ctx, RouteAttachments, krt.FilterIndex(RouteAttachmentsIndex, config.NamespacedName(i.Obj)))
		counts := map[string]int32{}
		for _, r := range tcpRoutes {
			counts[r.ListenerName] = counts[r.ListenerName] + 1
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
	registerStatus(GatewayFinalStatus, statusWriter)

	VirtualServices := krt.JoinCollection([]krt.Collection[config.Config]{
		tcpRoutes.VirtualServices,
		tlsRoutes.VirtualServices,
	}, opts.WithName("DerivedVirtualServices")...)

	outputs := Outputs{
		Gateways:        Gateways,
		VirtualServices: VirtualServices,
	}
	gatewayController.outputs = outputs

	return gatewayController
}

func buildClient[I controllers.ComparableObject](kc kube.Client, gvr schema.GroupVersionResource, opts krt.OptionsBuilder, name string) krt.Collection[I] {
	filter := kclient.Filter{
		ObjectFilter: kc.ObjectFilter(),
	}
	cc := kclient.NewDelayedInformer[I](kc, gvr, kubetypes.StandardInformer, filter)
	return krt.WrapClient[I](cc, opts.WithName(name)...)
}

func (c *Controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.VirtualService,
		collections.Gateway,
	)
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if typ != gvk.Gateway && typ != gvk.VirtualService {
		return nil
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	switch typ {
	case gvk.Gateway:
		return slices.MapFilter(c.outputs.Gateways.List(), func(g Gateway) *config.Config {
			if g.Valid {
				return &g.Config
			}
			return nil
		})
	case gvk.VirtualService:
		return c.outputs.VirtualServices.List()
	default:
		return nil
	}
}

func (c *Controller) SetStatusWrite(enabled bool, statusManager *status.Manager) {
	if enabled && features.EnableGatewayAPIStatus && statusManager != nil {
		c.setStatusQueue(statusManager.CreateGenericController(func(status status.Manipulator, context any) {
			status.SetInner(context)
		}))
	} else {
		c.statusWriter.statusController.Store(nil)
	}
}

func (c *Controller) setStatusQueue(queue status.Queue) {
	c.statusWriter.statusController.Store(&queue)
	log.Errorf("howardjohn: run resync %v", len(c.statusWriter.resyncers))
	for _, rs := range c.statusWriter.resyncers {
		rs()
	}
}

// Reconcile takes in a current snapshot of the gateway-api configs, and regenerates our internal state.
// Any status updates required will be enqueued as well.
func (c *Controller) Reconcile(ps *model.PushContext) {
	ctx := NewGatewayContext(ps, c.cluster)
	old := c.gatewayContext.Swap(&ctx)
	if old == nil {
		c.gatewayContextTrigger.MarkSynced()
	}
	return
	/*
		t0 := time.Now()
		defer func() {
			log.Debugf("reconcile complete in %v", time.Since(t0))
		}()
		gatewayClass := c.cache.List(gvk.GatewayClass, metav1.NamespaceAll)
		gateway := c.cache.List(gvk.KubernetesGateway, metav1.NamespaceAll)
		httpRoute := c.cache.List(gvk.HTTPRoute, metav1.NamespaceAll)
		grpcRoute := c.cache.List(gvk.GRPCRoute, metav1.NamespaceAll)
		tcpRoute := c.cache.List(gvk.TCPRoute, metav1.NamespaceAll)
		tlsRoute := c.cache.List(gvk.TLSRoute, metav1.NamespaceAll)
		referenceGrant := c.cache.List(gvk.ReferenceGrant, metav1.NamespaceAll)
		serviceEntry := c.cache.List(gvk.ServiceEntry, metav1.NamespaceAll) // TODO lazy load only referenced SEs?

		// all other types are filtered by revision, but for gateways we need to select tags as well
		gateway = slices.FilterInPlace(gateway, func(gw config.Config) bool {
			return c.tagWatcher.IsMine(gw.ToObjectMeta())
		})

		input := GatewayResources{
			GatewayClass:   deepCopyStatus(gatewayClass),
			Gateway:        deepCopyStatus(gateway),
			HTTPRoute:      deepCopyStatus(httpRoute),
			GRPCRoute:      deepCopyStatus(grpcRoute),
			TCPRoute:       deepCopyStatus(tcpRoute),
			TLSRoute:       deepCopyStatus(tlsRoute),
			ReferenceGrant: referenceGrant,
			ServiceEntry:   serviceEntry,
			Domain:         c.domain,
			Context:        NewGatewayContext(ps, c.cluster),
		}

		if !input.hasResources() {
			// Early exit for common case of no gateway-api used.
			c.stateMu.Lock()
			defer c.stateMu.Unlock()
			// make sure we clear out the state, to handle the last gateway-api resource being removed
			c.state = IstioResources{}
			return
		}

		nsl := c.namespaces.List("", klabels.Everything())
		namespaces := make(map[string]*corev1.Namespace, len(nsl))
		for _, ns := range nsl {
			namespaces[ns.Name] = ns
		}
		input.Namespaces = namespaces

		if c.credentialsController != nil {
			credentials, err := c.credentialsController.ForCluster(c.cluster)
			if err != nil {
				log.Warnf("failed to get credentials: %v", err)
			} else {
				input.Credentials = credentials
			}
		}

		output := convertResources(input)

		// Handle all status updates
		c.QueueStatusUpdates(input)

		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		c.state = output

	*/
}

type StatusWriter struct {
	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	statusController *atomic.Pointer[status.Queue]
	resyncers        []func()
}

func EnqueueStatus[T comparable](sw StatusWriter, obj controllers.Object, ws *kstatus.WrappedStatusTyped[T]) {
	if !ws.Dirty {
		return
	}
	statusController := sw.statusController.Load()
	if statusController == nil {
		return
	}

	// TODO: this is a bit awkward since the status controller is reading from crdstore. I suppose it works -- it just means
	// we cannot remove Gateway API types from there.
	res := status.Resource{
		GroupVersionResource: schematypes.GvrFromObject(obj),
		Namespace:            obj.GetNamespace(),
		Name:                 obj.GetName(),
		Generation:           strconv.FormatInt(obj.GetGeneration(), 10),
	}
	(*statusController).EnqueueStatusUpdateResource(ws.Unwrap(), res)
}

func EnqueueStatus2[T any](sw *StatusWriter, obj controllers.Object, ws T) {
	statusController := sw.statusController.Load()
	if statusController == nil {
		log.Errorf("howardjohn: no controller")
		return
	}

	// TODO: this is a bit awkward since the status controller is reading from crdstore. I suppose it works -- it just means
	// we cannot remove Gateway API types from there.
	res := status.Resource{
		GroupVersionResource: schematypes.GvrFromObject(obj),
		Namespace:            obj.GetNamespace(),
		Name:                 obj.GetName(),
		Generation:           strconv.FormatInt(obj.GetGeneration(), 10),
	}
	log.Errorf("howardjohn: ENQUEUE2 %v", res)
	(*statusController).EnqueueStatusUpdateResource(ws, res)
}

func (c *Controller) Create(config config.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Update(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", errUnsupportedOp
}

func (c *Controller) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return errUnsupportedOp
}

func (c *Controller) RegisterEventHandler(typ config.GroupVersionKind, handler model.EventHandler) {
	switch typ {
	case gvk.Namespace:
		c.namespaceHandler = handler
	case gvk.Secret:
		c.secretHandler = handler
	}
	// For all other types, do nothing as c.cache has been registered
}

func (c *Controller) Run(stop <-chan struct{}) {
	if features.EnableGatewayAPIGatewayClassController {
		go func() {
			if c.waitForCRD(gvr.GatewayClass, stop) {
				gcc := NewClassController(c.client)
				c.client.RunAndWait(stop)
				gcc.Run(stop)
			}
		}()
	}
	go c.tagWatcher.Run(stop)
	<-stop
	close(c.stop)
}

func (c *Controller) HasSynced() bool {
	return c.outputs.VirtualServices.HasSynced() && c.outputs.Gateways.HasSynced()
}

func (c *Controller) SecretAllowed(resourceName string, namespace string) bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state.AllowedReferences.SecretAllowed(resourceName, namespace)
}

// namespaceEvent handles a namespace add/update. Gateway's can select routes by label, so we need to handle
// when the labels change.
// Note: we don't handle delete as a delete would also clean up any relevant gateway-api types which will
// trigger its own event.
func (c *Controller) namespaceEvent(oldNs, newNs *corev1.Namespace) {
	// First, find all the label keys on the old/new namespace. We include NamespaceNameLabel
	// since we have special logic to always allow this on namespace.
	touchedNamespaceLabels := sets.New(NamespaceNameLabel)
	touchedNamespaceLabels.InsertAll(getLabelKeys(oldNs)...)
	touchedNamespaceLabels.InsertAll(getLabelKeys(newNs)...)

	// Next, we find all keys our Gateways actually reference.
	c.stateMu.RLock()
	intersection := touchedNamespaceLabels.IntersectInPlace(c.state.ReferencedNamespaceKeys)
	c.stateMu.RUnlock()

	// If there was any overlap, then a relevant namespace label may have changed, and we trigger a
	// push. A more exact check could actually determine if the label selection result actually changed.
	// However, this is a much simpler approach that is likely to scale well enough for now.
	if !intersection.IsEmpty() && c.namespaceHandler != nil {
		log.Debugf("namespace labels changed, triggering namespace handler: %v", intersection.UnsortedList())
		c.namespaceHandler(config.Config{}, config.Config{}, model.EventUpdate)
	}
}

// getLabelKeys extracts all label keys from a namespace object.
func getLabelKeys(ns *corev1.Namespace) []string {
	if ns == nil {
		return nil
	}
	return maps.Keys(ns.Labels)
}

func (c *Controller) secretEvent(name, namespace string) {
	var impactedConfigs []model.ConfigKey
	c.stateMu.RLock()
	impactedConfigs = c.state.ResourceReferences[model.ConfigKey{
		Kind:      kind.Secret,
		Namespace: namespace,
		Name:      name,
	}]
	c.stateMu.RUnlock()
	if len(impactedConfigs) > 0 {
		log.Debugf("secret %s/%s changed, triggering secret handler", namespace, name)
		for _, cfg := range impactedConfigs {
			gw := config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.KubernetesGateway,
					Namespace:        cfg.Namespace,
					Name:             cfg.Name,
				},
			}
			c.secretHandler(gw, gw, model.EventUpdate)
		}
	}
}

// deepCopyStatus creates a copy of all configs, with a copy of the status field that we can mutate.
// This allows our functions to call Status.Mutate, and then we can later persist all changes into the
// API server.
func deepCopyStatus(configs []config.Config) []config.Config {
	return slices.Map(configs, func(c config.Config) config.Config {
		return config.Config{
			Meta:   c.Meta,
			Spec:   c.Spec,
			Status: kstatus.Wrap(c.Status),
		}
	})
}

// filterNamespace allows filtering out configs to only a specific namespace. This allows implementing the
// List call which can specify a specific namespace.
func filterNamespace(cfgs []config.Config, namespace string) []config.Config {
	if namespace == metav1.NamespaceAll {
		return cfgs
	}
	return slices.Filter(cfgs, func(c config.Config) bool {
		return c.Namespace == namespace
	})
}

// hasResources determines if there are any gateway-api resources created at all.
// If not, we can short circuit all processing to avoid excessive work.
func (kr GatewayResources) hasResources() bool {
	return len(kr.GatewayClass) > 0 ||
		len(kr.Gateway) > 0 ||
		len(kr.HTTPRoute) > 0 ||
		len(kr.GRPCRoute) > 0 ||
		len(kr.TCPRoute) > 0 ||
		len(kr.TLSRoute) > 0 ||
		len(kr.ReferenceGrant) > 0
}
