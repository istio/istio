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
	"sync"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
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

type RouteContext struct {
	Krt krt.HandlerContext
	RouteContextInputs
}

type RouteContextInputs struct {
	Grants         ReferenceGrants
	RouteParents   RouteParents
	Domain         string
	Services       krt.Collection[*corev1.Service]
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry]
}

func (i RouteContextInputs) WithCtx(krtctx krt.HandlerContext) RouteContext {
	return RouteContext{
		Krt:                krtctx,
		RouteContextInputs: i,
	}
}

func registerStatus[I controllers.Object, IS any](statusCol krt.Collection[krt.ObjectWithStatus[I, IS]], statusWriter *StatusWriter) krt.Syncer {
	mu := sync.Mutex{}
	resync := func() {
		mu.Lock()
		defer mu.Unlock()
		items := statusCol.List()
		for _, l := range items {
			EnqueueStatus(statusWriter, l.Obj, &l.Status)
		}
	}
	statusWriter.resyncers = append(statusWriter.resyncers, resync)
	return statusCol.Register(func(o krt.Event[krt.ObjectWithStatus[I, IS]]) {
		mu.Lock()
		defer mu.Unlock()
		l := o.Latest()
		EnqueueStatus(statusWriter, l.Obj, &l.Status)
	})
}

type ParentInfo struct {
	Key  parentKey
	Info parentInfo
}

func (pi ParentInfo) ResourceName() string {
	return pi.Key.Name // TODO!!!! more infoi and section name
}

type TypedResource struct {
	Kind config.GroupVersionKind
	Name types.NamespacedName
}

type Outputs struct {
	Gateways        krt.Collection[Gateway]
	VirtualServices krt.Collection[config.Config]
	ReferenceGrants ReferenceGrants
}

type Inputs struct {
	Namespaces krt.Collection[*corev1.Namespace]

	Services krt.Collection[*corev1.Service]
	Secrets  krt.Collection[*corev1.Secret]

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
		tagWatcher:            revisions.NewTagWatcher(kc, options.Revision), // howardjohn: todo
		statusWriter:          statusWriter,
		waitForCRD:            waitForCRD,
		gatewayContext:        atomic.NewPointer[GatewayContext](nil),
		gatewayContextTrigger: krt.NewRecomputeTrigger(false, opts.WithName("gatewayContextTrigger")...),
		stop:                  stop,
	}

	inputs := Inputs{
		Namespaces: krt.NewInformer[*corev1.Namespace](kc, opts.WithName("Namespaces")...),
		Secrets: krt.WrapClient[*corev1.Secret](
			kclient.NewFiltered[*corev1.Secret](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("Secrets")...,
		),
		Services: krt.WrapClient[*corev1.Service](
			kclient.NewFiltered[*corev1.Service](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("Services")...,
		),
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
		inputs.Secrets,
		options.DomainSuffix,
		gatewayController.gatewayContext,
		gatewayController.gatewayContextTrigger,
		opts,
	)

	RouteParents := BuildRouteParents(Gateways)

	routeInputs := RouteContextInputs{
		Grants:         ReferenceGrants,
		RouteParents:   RouteParents,
		Domain:         options.DomainSuffix,
		Services:       inputs.Services,
		ServiceEntries: inputs.ServiceEntries,
	}
	tcpRoutes := TCPRouteCollection(
		inputs.TCPRoutes,
		routeInputs,
		opts,
	)
	registerStatus(tcpRoutes.Status, statusWriter)
	tlsRoutes := TLSRouteCollection(
		inputs.TLSRoutes,
		routeInputs,
		opts,
	)
	registerStatus(tlsRoutes.Status, statusWriter)
	httpRoutes := HTTPRouteCollection(
		inputs.HTTPRoutes,
		routeInputs,
		opts,
	)
	registerStatus(httpRoutes.Status, statusWriter)
	grpcRoutes := GRPCRouteCollection(
		inputs.GRPCRoutes,
		routeInputs,
		opts,
	)
	registerStatus(grpcRoutes.Status, statusWriter)

	RouteAttachments := krt.JoinCollection([]krt.Collection[RouteAttachment]{
		tcpRoutes.RouteAttachments,
		tlsRoutes.RouteAttachments,
		httpRoutes.RouteAttachments,
		grpcRoutes.RouteAttachments,
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
		httpRoutes.VirtualServices,
		grpcRoutes.VirtualServices,
	}, opts.WithName("DerivedVirtualServices")...)

	outputs := Outputs{
		ReferenceGrants: ReferenceGrants,
		Gateways:        Gateways,
		VirtualServices: VirtualServices,
	}
	gatewayController.outputs = outputs

	outputs.VirtualServices.RegisterBatch(pushXds(options.XDSUpdater,
		func(t config.Config) model.ConfigKey {
			return model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      t.Name,
				Namespace: t.Namespace,
			}
		}), false)

	outputs.Gateways.RegisterBatch(pushXds(options.XDSUpdater,
		func(t Gateway) model.ConfigKey {
			return model.ConfigKey{
				Kind:      kind.Gateway,
				Name:      t.Name,
				Namespace: t.Namespace,
			}
		}), false)
	// TODO: referencegrant update?

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
	c.gatewayContextTrigger.TriggerRecomputation()
	return
}

type StatusWriter struct {
	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	statusController *atomic.Pointer[status.Queue]
	resyncers        []func()
}

func EnqueueStatus[T any](sw *StatusWriter, obj controllers.Object, ws T) {
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
	return c.outputs.VirtualServices.HasSynced() &&
		c.outputs.Gateways.HasSynced() &&
		c.outputs.ReferenceGrants.collection.HasSynced()
}

func (c *Controller) SecretAllowed(resourceName string, namespace string) bool {
	return c.outputs.ReferenceGrants.SecretAllowed(nil, resourceName, namespace)
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

func pushXds[T any](xds model.XDSUpdater, f func(T) model.ConfigKey) func(events []krt.Event[T], initialSync bool) {
	return func(events []krt.Event[T], initialSync bool) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				c := f(i)
				if c != (model.ConfigKey{}) {
					cu.Insert(c)
				}
			}
		}
		if len(cu) == 0 {
			return
		}
		xds.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.ConfigUpdate),
		})
	}
}
