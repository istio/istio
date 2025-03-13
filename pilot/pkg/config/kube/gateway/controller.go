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

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	kubesecrets "istio.io/istio/pilot/pkg/credentials/kube"
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

	// the cluster where the gateway-api controller runs
	cluster  cluster.ID
	revision string

	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	status *StatusCollections

	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool
	outputs    Outputs

	gatewayContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]]
	tagWatcher     krt.RecomputeProtected[revisions.TagWatcher]

	stop       chan struct{}
	xdsUpdater model.XDSUpdater

	handlers []krt.HandlerRegistration
}

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
	Domain          string
	Services        krt.Collection[*corev1.Service]
	Namespaces      krt.Collection[*corev1.Namespace]
	ServiceEntries  krt.Collection[*networkingclient.ServiceEntry]
	internalContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]]
}

func (i RouteContextInputs) WithCtx(krtctx krt.HandlerContext) RouteContext {
	return RouteContext{
		Krt:                krtctx,
		RouteContextInputs: i,
	}
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
	VirtualServices krt.Collection[*config.Config]
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
	xdsUpdater model.XDSUpdater,
) *Controller {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "gateway", options.KrtDebugger)

	tw := revisions.NewTagWatcher(kc, options.Revision)
	c := &Controller{
		client:         kc,
		cluster:        options.ClusterID,
		revision:       options.Revision,
		status:         &StatusCollections{},
		tagWatcher:     krt.NewRecomputeProtected(tw, false, opts.WithName("tagWatcher")...),
		waitForCRD:     waitForCRD,
		gatewayContext: krt.NewRecomputeProtected(atomic.NewPointer[GatewayContext](nil), false, opts.WithName("gatewayContext")...),
		stop:           stop,
		xdsUpdater:     xdsUpdater,
	}
	tw.AddHandler(func(s sets.String) {
		c.tagWatcher.TriggerRecomputation()
	})

	inputs := Inputs{
		Namespaces: krt.NewInformer[*corev1.Namespace](kc, opts.WithName("informer/Namespaces")...),
		Secrets: krt.WrapClient[*corev1.Secret](
			kclient.NewFiltered[*corev1.Secret](kc, kubetypes.Filter{
				FieldSelector: kubesecrets.SecretsFieldSelector,
				ObjectFilter:  kc.ObjectFilter(),
			}),
			opts.WithName("informer/Secrets")...,
		),
		Services: krt.WrapClient[*corev1.Service](
			kclient.NewFiltered[*corev1.Service](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("informer/Services")...,
		),
		GatewayClasses:  buildClient[*gateway.GatewayClass](c, kc, gvr.GatewayClass, opts, "informer/GatewayClasses"),
		Gateways:        buildClient[*gateway.Gateway](c, kc, gvr.KubernetesGateway, opts, "informer/Gateways"),
		HTTPRoutes:      buildClient[*gateway.HTTPRoute](c, kc, gvr.HTTPRoute, opts, "informer/HTTPRoutes"),
		GRPCRoutes:      buildClient[*gatewayv1.GRPCRoute](c, kc, gvr.GRPCRoute, opts, "informer/GRPCRoutes"),
		TCPRoutes:       buildClient[*gatewayalpha.TCPRoute](c, kc, gvr.TCPRoute, opts, "informer/TCPRoutes"),
		TLSRoutes:       buildClient[*gatewayalpha.TLSRoute](c, kc, gvr.TLSRoute, opts, "informer/TLSRoutes"),
		ReferenceGrants: buildClient[*gateway.ReferenceGrant](c, kc, gvr.ReferenceGrant, opts, "informer/ReferenceGrants"),
		ServiceEntries:  buildClient[*networkingclient.ServiceEntry](c, kc, gvr.ServiceEntry, opts, "informer/ServiceEntries"),
	}

	handlers := []krt.HandlerRegistration{}

	GatewayClassStatus, GatewayClasses := GatewayClassesCollection(inputs.GatewayClasses, opts)
	registerStatus(c, GatewayClassStatus)

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
		c.gatewayContext,
		c.tagWatcher,
		opts,
	)

	RouteParents := BuildRouteParents(Gateways)

	routeInputs := RouteContextInputs{
		Grants:          ReferenceGrants,
		RouteParents:    RouteParents,
		Domain:          options.DomainSuffix,
		Services:        inputs.Services,
		Namespaces:      inputs.Namespaces,
		ServiceEntries:  inputs.ServiceEntries,
		internalContext: c.gatewayContext,
	}
	tcpRoutes := TCPRouteCollection(
		inputs.TCPRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, tcpRoutes.Status)
	tlsRoutes := TLSRouteCollection(
		inputs.TLSRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, tlsRoutes.Status)
	httpRoutes := HTTPRouteCollection(
		inputs.HTTPRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, httpRoutes.Status)
	grpcRoutes := GRPCRouteCollection(
		inputs.GRPCRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, grpcRoutes.Status)

	RouteAttachments := krt.JoinCollection([]krt.Collection[RouteAttachment]{
		tcpRoutes.RouteAttachments,
		tlsRoutes.RouteAttachments,
		httpRoutes.RouteAttachments,
		grpcRoutes.RouteAttachments,
	}, opts.WithName("RouteAttachments")...)
	RouteAttachmentsIndex := krt.NewIndex(RouteAttachments, func(o RouteAttachment) []types.NamespacedName {
		return []types.NamespacedName{o.To}
	})

	GatewayFinalStatus := krt.NewCollection(
		GatewaysStatus,
		func(ctx krt.HandlerContext, i krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus]) *krt.ObjectWithStatus[*gateway.Gateway, gateway.GatewayStatus] {
			tcpRoutes := krt.Fetch(ctx, RouteAttachments, krt.FilterIndex(RouteAttachmentsIndex, config.NamespacedName(i.Obj)))
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
	registerStatus(c, GatewayFinalStatus)

	VirtualServices := krt.JoinCollection([]krt.Collection[*config.Config]{
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
	c.outputs = outputs

	handlers = append(handlers,
		outputs.VirtualServices.RegisterBatch(pushXds(xdsUpdater,
			func(t *config.Config) model.ConfigKey {
				return model.ConfigKey{
					Kind:      kind.VirtualService,
					Name:      t.Name,
					Namespace: t.Namespace,
				}
			}), false),
		outputs.Gateways.RegisterBatch(pushXds(xdsUpdater,
			func(t Gateway) model.ConfigKey {
				return model.ConfigKey{
					Kind:      kind.Gateway,
					Name:      t.Name,
					Namespace: t.Namespace,
				}
			}), false),
		outputs.ReferenceGrants.collection.RegisterBatch(pushXds(xdsUpdater,
			func(t ReferenceGrant) model.ConfigKey {
				return model.ConfigKey{
					Kind:      kind.KubernetesGateway,
					Name:      t.Source.Name,
					Namespace: t.Source.Namespace,
				}
			}), false))
	c.handlers = handlers

	return c
}

func buildClient[I controllers.ComparableObject](
	c *Controller,
	kc kube.Client,
	res schema.GroupVersionResource,
	opts krt.OptionsBuilder,
	name string,
) krt.Collection[I] {
	filter := kclient.Filter{
		ObjectFilter: kubetypes.ComposeFilters(kc.ObjectFilter(), c.inRevision),
	}

	// all other types are filtered by revision, but for gateways we need to select tags as well
	if res == gvr.KubernetesGateway {
		filter.ObjectFilter = kc.ObjectFilter()
	}

	cc := kclient.NewDelayedInformer[I](kc, res, kubetypes.StandardInformer, filter)
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
		res := slices.MapFilter(c.outputs.Gateways.List(), func(g Gateway) *config.Config {
			if g.Valid {
				return g.Config
			}
			return nil
		})
		return res
	case gvk.VirtualService:
		return slices.Map(c.outputs.VirtualServices.List(), func(e *config.Config) config.Config {
			return *e
		})
	default:
		return nil
	}
}

func (c *Controller) SetStatusWrite(enabled bool, statusManager *status.Manager) {
	if enabled && features.EnableGatewayAPIStatus && statusManager != nil {
		var q status.Queue = statusManager.CreateGenericController(func(status status.Manipulator, context any) {
			status.SetInner(context)
		})
		c.status.SetQueue(q)
	} else {
		c.status.UnsetQueue()
	}
}

// Reconcile takes in a current snapshot of the gateway-api configs, and regenerates our internal state.
// Any status updates required will be enqueued as well.
func (c *Controller) Reconcile(ps *model.PushContext) {
	ctx := NewGatewayContext(ps, c.cluster)
	c.gatewayContext.Modify(func(i **atomic.Pointer[GatewayContext]) {
		(*i).Store(&ctx)
	})
	c.gatewayContext.MarkSynced()
}

func EnqueueStatus[T any](sw status.Queue, obj controllers.Object, ws T) {
	// TODO: this is a bit awkward since the status controller is reading from crdstore. I suppose it works -- it just means
	// we cannot remove Gateway API types from there.
	res := status.Resource{
		GroupVersionResource: schematypes.GvrFromObject(obj),
		Namespace:            obj.GetNamespace(),
		Name:                 obj.GetName(),
		Generation:           strconv.FormatInt(obj.GetGeneration(), 10),
	}
	sw.EnqueueStatusUpdateResource(ws, res)
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

	tw := c.tagWatcher.AccessUnprotected()
	go tw.Run(stop)
	go func() {
		kube.WaitForCacheSync("gateway tag watcher", stop, tw.HasSynced)
		c.tagWatcher.MarkSynced()
	}()

	<-stop
	close(c.stop)
}

func (c *Controller) HasSynced() bool {
	if !(c.outputs.VirtualServices.HasSynced() &&
		c.outputs.Gateways.HasSynced() &&
		c.outputs.ReferenceGrants.collection.HasSynced()) {
		return false
	}
	for _, h := range c.handlers {
		if !h.HasSynced() {
			return false
		}
	}
	return true
}

func (c *Controller) SecretAllowed(resourceName string, namespace string) bool {
	return c.outputs.ReferenceGrants.SecretAllowed(nil, resourceName, namespace)
}

func pushXds[T any](xds model.XDSUpdater, f func(T) model.ConfigKey) func(events []krt.Event[T]) {
	return func(events []krt.Event[T]) {
		if xds == nil {
			return
		}
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

func (c *Controller) inRevision(obj any) bool {
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	return config.LabelsInRevision(object.GetLabels(), c.revision)
}
