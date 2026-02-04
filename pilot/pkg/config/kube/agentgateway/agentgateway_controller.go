// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package agentgateway

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/agentgateway/agentgateway/go/api"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	kubesecrets "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

var logger = istiolog.RegisterScope("agentgateway", "agentgateway controller")

var errUnsupportedOp = errors.New("unsupported operation: the gateway config store is a read-only view")

// Controller defines the controller for agentgateway. The controller reads a variety of resources (Gateway types, as well
// as adjacent types like Namespace and Service).
//
// Most resources are fully "self-contained" with krt, but there are a few usages breaking out of `krt` for status reporting;
// these are managed by `krt.RecomputeProtected`. These are recomputed on each new PushContext initialization, which will
// call Controller.Reconcile().
//
// The status on all Gateway types is also tracked. Each collection emits downstream objects, but also status about the
// input type. If the status changes, it is queued to asynchronously update the status of the object in Kubernetes.
type Controller struct {
	// client for accessing Kubernetes
	client kube.Client

	// the cluster where the agentgateway controller runs
	cluster cluster.ID
	// revision the controller is running under
	revision string

	istioNamespace string

	// status controls the status writing queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	status *status.StatusCollections

	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool

	// gatewayContext exposes us to the internal Istio service registry. This is outside krt knowledge (currently), so,
	// so we wrap it in a RecomputeProtected.
	// Most usages in the API are directly referenced typed objects (Service, ServiceEntry, etc) so this is not needed typically.
	gatewayContext krt.RecomputeProtected[*atomic.Pointer[gatewaycommon.GatewayContext]]
	// tagWatcher allows us to check which tags are ours. Unlike most Istio codepaths, we read istio.io/rev=<tag> and not just
	// revisions for Gateways. This is because a Gateway is sort of a mix of a Deployment and Config.
	// Since the TagWatcher is not yet krt-aware, we wrap this in RecomputeProtected.
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher]

	stop chan struct{}

	// Handlers tracks all registered handlers, so that syncing can be detected
	handlers []krt.HandlerRegistration

	outputs OutputCollections

	inputs *AgwInputs

	domainSuffix string // the domain suffix to use for generated resources

	// features
	Registrations []xds.Registration
}

type TypedResource struct {
	Kind config.GroupVersionKind
	Name types.NamespacedName
}

// borrowed from kgateway syncer.go
type OutputCollections struct {
	Resources krt.Collection[AgwResource]
	Addresses krt.Collection[Address]
}

// Similar type to agwcollections in kgateway.
type AgwInputs struct {
	// Core k8s resources
	Namespaces     krt.Collection[*corev1.Namespace]
	Nodes          krt.Collection[*corev1.Node]
	Pods           krt.Collection[*corev1.Pod]
	Services       krt.Collection[*corev1.Service]
	Secrets        krt.Collection[*corev1.Secret]
	ConfigMaps     krt.Collection[*corev1.ConfigMap]
	EndpointSlices krt.Collection[*discovery.EndpointSlice]

	// Gateway API resources
	GatewayClasses       krt.Collection[*gatewayv1.GatewayClass]
	Gateways             krt.Collection[*gatewayv1.Gateway]
	HTTPRoutes           krt.Collection[*gatewayv1.HTTPRoute]
	GRPCRoutes           krt.Collection[*gatewayv1.GRPCRoute]
	TCPRoutes            krt.Collection[*gatewayalpha.TCPRoute]
	TLSRoutes            krt.Collection[*gatewayalpha.TLSRoute]
	ListenerSets         krt.Collection[*gatewayx.XListenerSet]
	ReferenceGrants      krt.Collection[*gateway.ReferenceGrant]
	BackendTrafficPolicy krt.Collection[*gatewayx.XBackendTrafficPolicy]
	BackendTLSPolicies   krt.Collection[*gatewayv1.BackendTLSPolicy]

	// Istio resources
	ServiceEntries  krt.Collection[*networkingclient.ServiceEntry]
	WorkloadEntries krt.Collection[*networkingclient.WorkloadEntry]

	// Extended resources
	InferencePools krt.Collection[*inferencev1.InferencePool]
}

var _ model.AgentgatewayController = &Controller{}

func NewAgwController(
	kc kube.Client,
	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool,
	options controller.Options,
) *Controller {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "agentgateway", options.KrtDebugger)

	tw := revisions.NewTagWatcher(kc, options.Revision, options.SystemNamespace)
	c := &Controller{
		client:         kc,
		cluster:        options.ClusterID,
		revision:       options.Revision,
		istioNamespace: options.SystemNamespace,
		status:         &status.StatusCollections{},
		tagWatcher:     krt.NewRecomputeProtected(tw, false, opts.WithName("tagWatcher")...),
		waitForCRD:     waitForCRD,
		gatewayContext: krt.NewRecomputeProtected(atomic.NewPointer[gatewaycommon.GatewayContext](nil), false, opts.WithName("gatewayContext")...),
		stop:           stop,
		domainSuffix:   options.DomainSuffix,
	}
	tw.AddHandler(func(s sets.String) {
		c.tagWatcher.TriggerRecomputation()
	})

	// TODO(jaellio): pass in inputs. Allowing reinitialization is risky
	c.initializeInputs(kc, opts)
	c.buildResourceCollections(opts)

	return c
}

func (c *Controller) initializeInputs(kc kube.Client, opts krt.OptionsBuilder) {
	if c.inputs != nil {
		return
	}
	inputs := &AgwInputs{
		Namespaces: krt.NewInformer[*corev1.Namespace](kc, opts.WithName("informer/Namespaces")...),
		Nodes: krt.NewFilteredInformer[*corev1.Node](kc, kclient.Filter{
			ObjectFilter: kc.ObjectFilter(),
		}, opts.WithName("informer/Nodes")...),
		Pods: krt.NewFilteredInformer[*corev1.Pod](kc, kclient.Filter{
			ObjectTransform: kube.StripPodUnusedFields,
			ObjectFilter:    kc.ObjectFilter(),
		}, opts.WithName("informer/Pods")...),
		Secrets: krt.WrapClient(
			kclient.NewFiltered[*corev1.Secret](kc, kubetypes.Filter{
				FieldSelector: kubesecrets.SecretsFieldSelector,
				ObjectFilter:  kc.ObjectFilter(),
			}),
			opts.WithName("informer/Secrets")...,
		),
		ConfigMaps: krt.WrapClient(
			kclient.NewFiltered[*corev1.ConfigMap](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("informer/ConfigMaps")...,
		),
		Services: krt.WrapClient(
			kclient.NewFiltered[*corev1.Service](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()}),
			opts.WithName("informer/Services")...,
		),
		GatewayClasses:     buildClient[*gatewayv1.GatewayClass](c, kc, gvr.GatewayClass, opts, "informer/GatewayClasses"),
		Gateways:           buildClient[*gatewayv1.Gateway](c, kc, gvr.KubernetesGateway, opts, "informer/Gateways"),
		HTTPRoutes:         buildClient[*gatewayv1.HTTPRoute](c, kc, gvr.HTTPRoute, opts, "informer/HTTPRoutes"),
		GRPCRoutes:         buildClient[*gatewayv1.GRPCRoute](c, kc, gvr.GRPCRoute, opts, "informer/GRPCRoutes"),
		BackendTLSPolicies: buildClient[*gatewayv1.BackendTLSPolicy](c, kc, gvr.BackendTLSPolicy, opts, "informer/BackendTLSPolicies"),

		ReferenceGrants: buildClient[*gateway.ReferenceGrant](c, kc, gvr.ReferenceGrant, opts, "informer/ReferenceGrants"),
		ServiceEntries:  buildClient[*networkingclient.ServiceEntry](c, kc, gvr.ServiceEntry, opts, "informer/ServiceEntries"),
		WorkloadEntries: buildClient[*networkingclient.WorkloadEntry](c, kc, gvr.WorkloadEntry, opts, "informer/WorkloadEntries"),
		EndpointSlices: krt.NewFilteredInformer[*discovery.EndpointSlice](kc, kclient.Filter{
			ObjectFilter: kc.ObjectFilter(),
		}, opts.WithName("informer/EndpointSlices")...),
	}

	if features.EnableAlphaGatewayAPI {
		inputs.TCPRoutes = buildClient[*gatewayalpha.TCPRoute](c, kc, gvr.TCPRoute, opts, "informer/TCPRoutes")
		inputs.TLSRoutes = buildClient[*gatewayalpha.TLSRoute](c, kc, gvr.TLSRoute, opts, "informer/TLSRoutes")
		inputs.BackendTrafficPolicy = buildClient[*gatewayx.XBackendTrafficPolicy](c, kc, gvr.XBackendTrafficPolicy, opts, "informer/XBackendTrafficPolicy")
		inputs.ListenerSets = buildClient[*gatewayx.XListenerSet](c, kc, gvr.XListenerSet, opts, "informer/XListenerSet")
	} else {
		// If disabled, still build a collection but make it always empty
		inputs.TCPRoutes = krt.NewStaticCollection[*gatewayalpha.TCPRoute](nil, nil, opts.WithName("disable/TCPRoutes")...)
		inputs.TLSRoutes = krt.NewStaticCollection[*gatewayalpha.TLSRoute](nil, nil, opts.WithName("disable/TLSRoutes")...)
		inputs.BackendTrafficPolicy = krt.NewStaticCollection[*gatewayx.XBackendTrafficPolicy](nil, nil, opts.WithName("disable/XBackendTrafficPolicy")...)
		inputs.ListenerSets = krt.NewStaticCollection[*gatewayx.XListenerSet](nil, nil, opts.WithName("disable/XListenerSet")...)
	}

	// TODO(jeallio): This has to be enabled, so remove conditional check?
	if features.EnableGatewayAPIInferenceExtension {
		inputs.InferencePools = buildClient[*inferencev1.InferencePool](c, kc, gvr.InferencePool, opts, "informer/InferencePools")
	} else {
		// If disabled, still build a collection but make it always empty
		logger.Warnf("GatewayAPI Inference Extension is disabled, not watching InferencePool resources")
		inputs.InferencePools = krt.NewStaticCollection[*inferencev1.InferencePool](nil, nil, opts.WithName("disable/InferencePools")...)
	}
	c.inputs = inputs
}

// TODO(jaellio): Consider refactoring so actual collection creation happen in a common BaseGatewayController type so
// collections are only built once across controller and agentgateway_controller
func (c *Controller) buildResourceCollections(opts krt.OptionsBuilder) {
	gatewayClassStatus, gatewayClasses := gatewaycommon.GatewayClassesCollection(c.inputs.GatewayClasses, opts)
	status.RegisterStatus(c.status, gatewayClassStatus, GetStatus, c.tagWatcher.AccessUnprotected())

	referenceGrants := gatewaycommon.BuildReferenceGrants(gatewaycommon.ReferenceGrantsCollection(c.inputs.ReferenceGrants, opts))
	listenerSetStatus, listenerSets := ListenerSetCollection(
		c.inputs.ListenerSets,
		c.inputs.Gateways,
		gatewayClasses,
		c.inputs.Namespaces,
		referenceGrants,
		c.inputs.ConfigMaps,
		c.inputs.Secrets,
		c.domainSuffix,
		c.gatewayContext,
		c.tagWatcher,
		opts,
	)
	status.RegisterStatus(c.status, listenerSetStatus, GetStatus, c.tagWatcher.AccessUnprotected())
	// GatewaysStatus is not fully complete until its join with route attachments to report attachedRoutes.
	// Do not register yet.
	// GatewayAPI uses status - how you know what
	gatewayInitialStatus, gateways := GatewayCollection(
		c.inputs.Gateways,
		listenerSets,
		gatewayClasses,
		c.inputs.Namespaces,
		referenceGrants,
		c.inputs.ConfigMaps,
		c.inputs.Secrets,
		c.domainSuffix,
		c.gatewayContext,
		c.tagWatcher,
		opts,
	)

	// Build agw resources for gateway
	agwResources, routeAttachments := c.buildAgwResources(gateways, referenceGrants, opts)

	gatewayFinalStatus := c.buildFinalGatewayStatus(gatewayInitialStatus, routeAttachments, opts)
	status.RegisterStatus(c.status, gatewayFinalStatus, GetStatus, c.tagWatcher.AccessUnprotected())

	httpRoutesByInferencePool := krt.NewIndex(c.inputs.HTTPRoutes, "inferencepool-route", indexHTTPRouteByInferencePool)
	InferencePoolStatus, _ := InferencePoolCollection(
		c.inputs.InferencePools,
		c.inputs.Services,
		c.inputs.HTTPRoutes,
		c.inputs.Gateways,
		httpRoutesByInferencePool,
		c,
		opts,
	)
	if features.EnableGatewayAPIInferenceExtension {
		status.RegisterStatus(c.status, InferencePoolStatus, GetStatus, c.tagWatcher.AccessUnprotected())
	}

	// TODO(jaellio): Source addresses from the ambientindex so the agentgateway proxies get the same
	// representation of addresses as ambient proxies (for multicluster)
	// Build address collections
	addresses := c.buildAddressCollections(opts)

	// Build XDS collection
	c.buildXDSCollection(agwResources, addresses, opts)

	// TODO(jaellio): Determine what additional sync dependencies are needed in addition to WaitForCacheSync
	// c.setupSyncDependencies(agwResources, addresses)
	c.outputs.Resources = agwResources
	c.outputs.Addresses = addresses
}

func (c *Controller) buildFinalGatewayStatus(
	gatewayStatuses krt.StatusCollection[*gatewayv1.Gateway, gatewayv1.GatewayStatus],
	routeAttachments krt.Collection[*RouteAttachment],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gatewayv1.Gateway, gatewayv1.GatewayStatus] {
	routeAttachmentsIndex := krt.NewIndex(routeAttachments, "to", func(o *RouteAttachment) []types.NamespacedName {
		return []types.NamespacedName{o.To}
	})
	return krt.NewCollection(
		gatewayStatuses,
		func(ctx krt.HandlerContext, i krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus]) *krt.ObjectWithStatus[*gatewayv1.Gateway, gatewayv1.GatewayStatus] {
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

func (c *Controller) buildAddressCollections(opts krt.OptionsBuilder) krt.Collection[Address] {
	inputs := c.inputs
	clusterId := cluster.ID(c.cluster)
	Networks := ambient.BuildNetworkCollections(inputs.Namespaces, inputs.Gateways, ambient.Options{
		SystemNamespace: c.istioNamespace,
		ClusterID:       clusterId,
	}, opts)
	builder := ambient.Builder{
		DomainSuffix:      c.domainSuffix,
		ClusterID:         clusterId,
		NetworkGateways:   Networks.NetworkGateways,
		GatewaysByNetwork: Networks.GatewaysByNetwork,
		Flags: ambient.FeatureFlags{
			EnableK8SServiceSelectWorkloadEntries: true,
		},
		Network: func(ctx krt.HandlerContext) network.ID {
			return ""
		},
	}
	// Dummy empty mesh config
	meshConfig := krt.NewStatic[ambient.MeshConfig](&ambient.MeshConfig{MeshConfig: mesh.DefaultMeshConfig()}, true, opts.WithName("MeshConfig")...)

	waypoints := builder.WaypointsCollection(clusterId, inputs.Gateways, inputs.GatewayClasses, inputs.Pods, opts)
	services := builder.ServicesCollection(
		clusterId,
		inputs.Services,
		inputs.ServiceEntries,
		waypoints,
		inputs.Namespaces,
		meshConfig,
		opts,
		true,
	)

	// TODO(jaellio): Is this the correct domain suffix?
	inferencePoolsInfo := krt.NewCollection(inputs.InferencePools, inferencePoolBuilder(c.domainSuffix),
		opts.WithName("InferencePools")...)
	services = krt.JoinCollection([]krt.Collection[model.ServiceInfo]{services, inferencePoolsInfo}, krt.WithJoinUnchecked())

	nodeLocality := ambient.NodesCollection(inputs.Nodes, opts.WithName("NodeLocality")...)
	workloads := builder.WorkloadsCollection(
		inputs.Pods,
		nodeLocality, // NodeLocality,
		meshConfig,
		// Authz/Authn are not use for agentgateway, ignore
		krt.NewStaticCollection[model.WorkloadAuthorization](nil, nil),
		krt.NewStaticCollection[*securityclient.PeerAuthentication](nil, nil),
		waypoints,
		services,
		inputs.WorkloadEntries,
		inputs.ServiceEntries,
		inputs.EndpointSlices,
		inputs.Namespaces,
		opts,
	)

	// Build address collections
	workloadAddresses := krt.MapCollection(workloads, func(t model.WorkloadInfo) Address {
		return Address{Workload: &t}
	})
	svcAddresses := krt.MapCollection(services, func(t model.ServiceInfo) Address {
		return Address{Service: &t}
	})

	adpAddresses := krt.JoinCollection([]krt.Collection[Address]{svcAddresses, workloadAddresses}, opts.WithName("Addresses")...)
	return adpAddresses
}

func (c *Controller) buildXDSCollection(
	agwResources krt.Collection[AgwResource],
	xdsAddresses krt.Collection[Address],
	opts krt.OptionsBuilder,
) {
	// Used to create an index on adpResources by Gateway to avoid fetching all resources
	agwResourcesByGateway := func(resource AgwResource) types.NamespacedName {
		return resource.Gateway
	}
	c.Registrations = append(c.Registrations, xds.Collection[Address, *workloadapi.Address](xdsAddresses, opts))
	c.Registrations = append(c.Registrations, xds.PerGatewayCollection[AgwResource, *api.Resource](agwResources, agwResourcesByGateway, opts))
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

// TODO(jaellio): Remove or update for agentgateway output collection types
func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	return nil
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
	ctx := gatewaycommon.NewGatewayContext(ps, c.cluster)
	c.gatewayContext.Modify(func(i **atomic.Pointer[gatewaycommon.GatewayContext]) {
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
				gcc := gatewaycommon.NewClassController(c.client, gatewaycommon.ClassControllerOptions{})
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
	return c.outputs.Addresses.HasSynced() &&
		c.outputs.Resources.HasSynced()
}

func (c *Controller) inRevision(obj any) bool {
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	return config.LabelsInRevision(object.GetLabels(), c.revision)
}

// ToAgwResource converts an internal representation to a resource for agentgateway
func ToAgwResource(t any) *api.Resource {
	switch tt := t.(type) {
	case *api.Bind:
		return &api.Resource{Kind: &api.Resource_Bind{Bind: tt}}
	case *api.Listener:
		return &api.Resource{Kind: &api.Resource_Listener{Listener: tt}}
	case *api.Route:
		return &api.Resource{Kind: &api.Resource_Route{Route: tt}}
	case *api.TCPRoute:
		return &api.Resource{Kind: &api.Resource_TcpRoute{TcpRoute: tt}}
	case *api.Policy:
		return &api.Resource{Kind: &api.Resource_Policy{Policy: tt}}
	case *api.Resource:
		return tt
	}
	// borrowed from kgateway gateway_collection.go
	panic(fmt.Sprintf("unknown resource kind %T", t))
}

func ToResourceForGateway(gw types.NamespacedName, resource any) AgwResource {
	return AgwResource{
		Resource: ToAgwResource(resource),
		Gateway:  gw,
	}
}

func (c *Controller) buildAgwResources(
	gateways krt.Collection[*GatewayListener],
	refGrants gatewaycommon.ReferenceGrants,
	opts krt.OptionsBuilder,
) (
	krt.Collection[AgwResource],
	krt.Collection[*RouteAttachment],
) {
	// filter gateway collections to only include gateways which use a built-in gateway class
	// (resources for additional gateway classes should be created by the downstream providing them)
	filteredGateways := krt.NewCollection(gateways, func(ctx krt.HandlerContext, gw *GatewayListener) **GatewayListener {
		// Note: This filtering logic is opposite of kgateway which uses additionalGatewayClasses
		if _, builtInClass := gatewaycommon.BuiltinClasses[gatewayv1.ObjectName(gw.Name)]; !builtInClass {
			return nil
		}
		return &gw
	}, opts.WithName("FilteredGateways")...)

	// Build ports and binds
	ports := krt.UnnamedIndex(filteredGateways, func(l *GatewayListener) []string {
		return []string{fmt.Sprint(l.ParentInfo.Port)}
	}).AsCollection(opts.WithName("PortBindings")...)

	binds := krt.NewManyCollection(ports, func(ctx krt.HandlerContext, object krt.IndexObject[string, *GatewayListener]) []AgwResource {
		port, _ := strconv.Atoi(object.Key)
		uniq := sets.New[types.NamespacedName]()
		var protocol = api.Bind_Protocol(0)
		for _, gw := range object.Objects {
			uniq.Insert(types.NamespacedName{
				Namespace: gw.ParentGateway.Namespace,
				Name:      gw.ParentGateway.Name,
			})
			// TODO: better handle conflicts of protocols. For now, we arbitrarily treat TLS > plain
			if gw.Valid {
				protocol = max(protocol, c.getBindProtocol(gw))
			}
		}
		return slices.Map(uniq.UnsortedList(), func(e types.NamespacedName) AgwResource {
			bind := &api.Bind{
				Key:      object.Key + "/" + e.String(),
				Port:     uint32(port), //nolint:gosec // G115: port is always in valid port range
				Protocol: protocol,
			}
			return ToResourceForGateway(e, bind)
		})
	}, opts.WithName("Binds")...)

	// Build listeners
	listeners := krt.NewCollection(filteredGateways, func(ctx krt.HandlerContext, obj *GatewayListener) *AgwResource {
		return c.buildListenerFromGateway(obj)
	}, opts.WithName("Listeners")...)

	// Build routes
	routeParents := BuildRouteParents(filteredGateways)

	routeInputs := RouteContextInputs{
		Grants:         refGrants,
		RouteParents:   routeParents,
		Services:       c.inputs.Services,
		Namespaces:     c.inputs.Namespaces,
		ServiceEntries: c.inputs.ServiceEntries,
		InferencePools: c.inputs.InferencePools,
	}

	// TODO(jaellio): Should the tag watcher be registered with dependents?
	agwRoutes, routeAttachments := AgwRouteCollection(c.status, c.inputs.HTTPRoutes, c.inputs.GRPCRoutes, c.inputs.TCPRoutes, c.inputs.TLSRoutes, routeInputs, c.tagWatcher, opts)

	// Join all Agw resources
	allAgwResources := krt.JoinCollection([]krt.Collection[AgwResource]{binds, listeners, agwRoutes}, opts.WithName("Resources")...)

	return allAgwResources, routeAttachments
}

// Taken from kgateway utils.go
func ListenerName(namespace, name string, listener string) *api.ListenerName {
	return &api.ListenerName{
		GatewayName:      name,
		GatewayNamespace: namespace,
		ListenerName:     listener,
		ListenerSet:      nil,
	}
}

// buildListenerFromGateway creates a listener resource from a gateway
func (c *Controller) buildListenerFromGateway(obj *GatewayListener) *AgwResource {
	l := &api.Listener{
		Key:      obj.ResourceName(),
		Name:     ListenerName(obj.ParentObject.Namespace, obj.ParentObject.Name, string(obj.ParentInfo.SectionName)),
		BindKey:  fmt.Sprint(obj.ParentInfo.Port) + "/" + obj.ParentObject.Namespace + "/" + obj.ParentObject.Name,
		Hostname: obj.ParentInfo.OriginalHostname,
	}

	// Set protocol and TLS configuration
	protocol, tlsConfig, ok := c.getProtocolAndTLSConfig(obj)
	if !ok {
		return nil // Unsupported protocol or missing TLS config
	}

	l.Protocol = protocol
	l.Tls = tlsConfig

	return ptr.Of(ToResourceForGateway(types.NamespacedName{
		Namespace: obj.ParentObject.Namespace,
		Name:      obj.ParentObject.Name,
	}, l))
}

// getProtocolAndTLSConfig extracts protocol and TLS configuration from a gateway
func (c *Controller) getProtocolAndTLSConfig(obj *GatewayListener) (api.Protocol, *api.TLSConfig, bool) {
	var tlsConfig *api.TLSConfig

	// Build TLS config if needed
	if obj.TLSInfo != nil {
		tlsConfig = &api.TLSConfig{
			Cert:       obj.TLSInfo.Cert,
			PrivateKey: obj.TLSInfo.Key,
		}
		if len(obj.TLSInfo.CaCert) > 0 {
			tlsConfig.Root = obj.TLSInfo.CaCert
		}
	}

	switch obj.ParentInfo.Protocol {
	case gatewayv1.HTTPProtocolType:
		return api.Protocol_HTTP, nil, true
	case gatewayv1.HTTPSProtocolType:
		if tlsConfig == nil {
			return api.Protocol_HTTPS, nil, false // TLS required but not configured
		}
		return api.Protocol_HTTPS, tlsConfig, true
	case gatewayv1.TLSProtocolType:
		if tlsConfig == nil {
			if obj.ParentInfo.TLSPassthrough {
				// For passthrough, we don't want TLS config
				return api.Protocol_TLS, nil, true
			} else {
				// TLS required but not configured
				return api.Protocol_TLS, nil, false
			}
		}
		return api.Protocol_TLS, tlsConfig, true
	case gatewayv1.TCPProtocolType:
		return api.Protocol_TCP, nil, true
	default:
		return api.Protocol_HTTP, nil, false // Unsupported protocol
	}
}

// getBindProtocol extracts the bind protocol from a gateway
func (c *Controller) getBindProtocol(obj *GatewayListener) api.Bind_Protocol {
	switch obj.ParentInfo.Protocol {
	case gatewayv1.HTTPProtocolType:
		return api.Bind_HTTP
	case gatewayv1.HTTPSProtocolType:
		return api.Bind_TLS
	case gatewayv1.TLSProtocolType:
		return api.Bind_TLS
	case gatewayv1.TCPProtocolType:
		return api.Bind_TCP
	default:
		return api.Bind_HTTP
	}
}
