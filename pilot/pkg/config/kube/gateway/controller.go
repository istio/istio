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

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

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

// Controller defines the controller for the gateway-api. The controller reads a variety of resources (Gateway types, as well
// as adjacent types like Namespace and Service), and through `krt`, translates them into Istio types (Gateway/VirtualService).
//
// Most resources are fully "self-contained" with krt, but there are a few usages breaking out of `krt`; these are managed by `krt.RecomputeProtected`.
// These are recomputed on each new PushContext initialization, which will call Controller.Reconcile().
//
// The generated Istio types are not stored in the cluster at all and are purely internal. Calls to List() (from PushContext)
// will expose these. They can be introspected at /debug/configz.
//
// The status on all gateway-api types is also tracked. Each collection emits downstream objects, but also status about the
// input type. If the status changes, it is queued to asynchronously update the status of the object in Kubernetes.
type Controller struct {
	// client for accessing Kubernetes
	client kube.Client

	// the cluster where the gateway-api controller runs
	cluster cluster.ID
	// revision the controller is running under
	revision string

	// status controls the status writing queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	status *StatusCollections

	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool

	// gatewayContext exposes us to the internal Istio service registry. This is outside krt knowledge (currently), so,
	// so we wrap it in a RecomputeProtected.
	// Most usages in the API are directly referenced typed objects (Service, ServiceEntry, etc) so this is not needed typically.
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[GatewayContext]]
	// tagWatcher allows us to check which tags are ours. Unlike most Istio codepaths, we read istio.io/rev=<tag> and not just
	// revisions for Gateways. This is because a Gateway is sort of a mix of a Deployment and Config.
	// Since the TagWatcher is not yet krt-aware, we wrap this in RecomputeProtected.
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher]

	stop chan struct{}

	xdsUpdater model.XDSUpdater

	// Handlers tracks all registered handlers, so that syncing can be detected
	handlers []krt.HandlerRegistration

	// outputs contains all the output collections for this controller.
	// Currently, the only usage of this controller is from non-krt things (PushContext) so this is not exposed directly.
	// If desired in the future, it could be.
	outputs Outputs
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
	Gateways         krt.Collection[Gateway]
	VirtualServices  krt.Collection[*config.Config]
	ReferenceGrants  ReferenceGrants
	DestinationRules krt.Collection[*config.Config]
}

type Inputs struct {
	Namespaces krt.Collection[*corev1.Namespace]

	Services   krt.Collection[*corev1.Service]
	Secrets    krt.Collection[*corev1.Secret]
	ConfigMaps krt.Collection[*corev1.ConfigMap]

	GatewayClasses       krt.Collection[*gateway.GatewayClass]
	Gateways             krt.Collection[*gateway.Gateway]
	HTTPRoutes           krt.Collection[*gateway.HTTPRoute]
	GRPCRoutes           krt.Collection[*gatewayv1.GRPCRoute]
	TCPRoutes            krt.Collection[*gatewayalpha.TCPRoute]
	TLSRoutes            krt.Collection[*gatewayalpha.TLSRoute]
	ReferenceGrants      krt.Collection[*gateway.ReferenceGrant]
	BackendTrafficPolicy krt.Collection[*gatewayx.XBackendTrafficPolicy]
	BackendTLSPolicies   krt.Collection[*gatewayalpha3.BackendTLSPolicy]
	ServiceEntries       krt.Collection[*networkingclient.ServiceEntry]
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
		ConfigMaps: krt.WrapClient[*corev1.ConfigMap](
			kclient.NewFiltered[*corev1.ConfigMap](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("informer/ConfigMaps")...,
		),
		Services: krt.WrapClient[*corev1.Service](
			kclient.NewFiltered[*corev1.Service](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("informer/Services")...,
		),
		GatewayClasses: buildClient[*gateway.GatewayClass](c, kc, gvr.GatewayClass, opts, "informer/GatewayClasses"),
		Gateways:       buildClient[*gateway.Gateway](c, kc, gvr.KubernetesGateway, opts, "informer/Gateways"),
		HTTPRoutes:     buildClient[*gateway.HTTPRoute](c, kc, gvr.HTTPRoute, opts, "informer/HTTPRoutes"),
		GRPCRoutes:     buildClient[*gatewayv1.GRPCRoute](c, kc, gvr.GRPCRoute, opts, "informer/GRPCRoutes"),

		ReferenceGrants: buildClient[*gateway.ReferenceGrant](c, kc, gvr.ReferenceGrant, opts, "informer/ReferenceGrants"),
		ServiceEntries:  buildClient[*networkingclient.ServiceEntry](c, kc, gvr.ServiceEntry, opts, "informer/ServiceEntries"),
	}
	if features.EnableAlphaGatewayAPI {
		inputs.TCPRoutes = buildClient[*gatewayalpha.TCPRoute](c, kc, gvr.TCPRoute, opts, "informer/TCPRoutes")
		inputs.TLSRoutes = buildClient[*gatewayalpha.TLSRoute](c, kc, gvr.TLSRoute, opts, "informer/TLSRoutes")
		inputs.BackendTLSPolicies = buildClient[*gatewayalpha3.BackendTLSPolicy](c, kc, gvr.BackendTLSPolicy, opts, "informer/BackendTLSPolicies")
		inputs.BackendTrafficPolicy = buildClient[*gatewayx.XBackendTrafficPolicy](c, kc, gvr.XBackendTrafficPolicy, opts, "informer/XBackendTrafficPolicy")
	} else {
		// If disabled, still build a collection but make it always empty
		inputs.TCPRoutes = krt.NewStaticCollection[*gatewayalpha.TCPRoute](nil, nil, opts.WithName("disable/TCPRoutes")...)
		inputs.TLSRoutes = krt.NewStaticCollection[*gatewayalpha.TLSRoute](nil, nil, opts.WithName("disable/TLSRoutes")...)
		inputs.BackendTLSPolicies = krt.NewStaticCollection[*gatewayalpha3.BackendTLSPolicy](nil, nil, opts.WithName("disable/BackendTLSPolicies")...)
		inputs.BackendTrafficPolicy = krt.NewStaticCollection[*gatewayx.XBackendTrafficPolicy](nil, nil, opts.WithName("disable/XBackendTrafficPolicy")...)
	}

	references := NewReferenceSet(
		AddReference(inputs.Services),
		AddReference(inputs.ConfigMaps),
		AddReference(inputs.Secrets),
	)

	handlers := []krt.HandlerRegistration{}

	GatewayClassStatus, GatewayClasses := GatewayClassesCollection(inputs.GatewayClasses, opts)
	registerStatus(c, GatewayClassStatus, GetStatus)

	ReferenceGrants := BuildReferenceGrants(ReferenceGrantsCollection(inputs.ReferenceGrants, opts))

	DestinationRules := DestinationRuleCollection(
		inputs.BackendTrafficPolicy,
		inputs.BackendTLSPolicies,
		references,
		options.DomainSuffix,
		c,
		opts,
	)

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
		DomainSuffix:    options.DomainSuffix,
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
	registerStatus(c, tcpRoutes.Status, GetStatus)
	tlsRoutes := TLSRouteCollection(
		inputs.TLSRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, tlsRoutes.Status, GetStatus)
	httpRoutes := HTTPRouteCollection(
		inputs.HTTPRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, httpRoutes.Status, GetStatus)
	grpcRoutes := GRPCRouteCollection(
		inputs.GRPCRoutes,
		routeInputs,
		opts,
	)
	registerStatus(c, grpcRoutes.Status, GetStatus)

	RouteAttachments := krt.JoinCollection([]krt.Collection[RouteAttachment]{
		tcpRoutes.RouteAttachments,
		tlsRoutes.RouteAttachments,
		httpRoutes.RouteAttachments,
		grpcRoutes.RouteAttachments,
	}, opts.WithName("RouteAttachments")...)
	RouteAttachmentsIndex := krt.NewIndex(RouteAttachments, func(o RouteAttachment) []types.NamespacedName {
		return []types.NamespacedName{o.To}
	})

	GatewayFinalStatus := FinalGatewayStatusCollection(GatewaysStatus, RouteAttachments, RouteAttachmentsIndex, opts)
	registerStatus(c, GatewayFinalStatus, GetStatus)

	VirtualServices := krt.JoinCollection([]krt.Collection[*config.Config]{
		tcpRoutes.VirtualServices,
		tlsRoutes.VirtualServices,
		httpRoutes.VirtualServices,
		grpcRoutes.VirtualServices,
	}, opts.WithName("DerivedVirtualServices")...)

	outputs := Outputs{
		ReferenceGrants:  ReferenceGrants,
		Gateways:         Gateways,
		VirtualServices:  VirtualServices,
		DestinationRules: DestinationRules,
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
		outputs.DestinationRules.RegisterBatch(pushXds(xdsUpdater,
			func(t *config.Config) model.ConfigKey {
				return model.ConfigKey{
					Kind:      kind.DestinationRule,
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
			}), false))
	c.handlers = handlers

	return c
}

// buildClient is a small wrapper to build a krt collection based on a delayed informer.
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
		collections.DestinationRule,
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
	case gvk.DestinationRule:
		return slices.Map(c.outputs.DestinationRules.List(), func(e *config.Config) config.Config {
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

// Reconcile is called each time the `gatewayContext` may change. We use this to mark it as updated.
func (c *Controller) Reconcile(ps *model.PushContext) {
	ctx := NewGatewayContext(ps, c.cluster)
	c.gatewayContext.Modify(func(i **atomic.Pointer[GatewayContext]) {
		(*i).Store(&ctx)
	})
	c.gatewayContext.MarkSynced()
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
	// We do not do event handler registration this way, and instead directly call the XDS Updated.
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
		c.outputs.DestinationRules.HasSynced() &&
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
