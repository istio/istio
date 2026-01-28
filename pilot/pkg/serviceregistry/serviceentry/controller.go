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

package serviceentry

import (
	"cmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var (
	_   serviceregistry.Instance = &Controller{}
	log                          = istiolog.RegisterScope("serviceentry", "ServiceEntry registry")
)

type networkIDCallback func(endpointIP string, labels labels.Instance) network.ID

// Controller communicates with ServiceEntry CRDs and monitors for changes.
type Controller struct {
	XdsUpdater model.XDSUpdater

	store     model.ConfigStore
	clusterID cluster.ID

	stop        chan struct{}
	krtDebugger *krt.DebugHandler
	opts        krt.OptionsBuilder
	inputs      Inputs
	outputs     Outputs
	handlers    []krt.HandlerRegistration

	workloadHandlers []func(*model.WorkloadInstance, model.Event)

	// callback function used to get the networkID according to workload ip and labels.
	// TODO: migrate this callback to a dependency on a KRT collection
	networkIDCallback networkIDCallback

	// Indicates whether this controller is for workload entries.
	workloadEntryController bool

	model.NoopAmbientIndexes
	model.NetworkGatewaysHandler
}

type Inputs struct {
	MeshConfig      krt.Collection[meshwatcher.MeshConfigResource]
	WorkloadEntries krt.Collection[config.Config]
	ServiceEntries  krt.Collection[config.Config]
	// TODO: this should be a joined collection with multi cluster workloads
	ExternalWorkloads krt.StaticCollection[*model.WorkloadInstance]
}

type Outputs struct {
	// Services is a collection of unique services with their corresponding instances,
	// these instances include ServiceEntry inlined WorkloadEntries as well as selected
	// instances based on WorkloadSelector.
	// Use cases:
	// - source of truth for controller services.
	// - XDS ConfigUpdates for service updates.
	Services       krt.Collection[ServiceWithInstances]
	ServicesByHost krt.Index[string, ServiceWithInstances]
	// ServiceInstancesByNamespaceHost is a collection of service instances keyed by namespace and hostname.
	// Use cases:
	// - as an input to EDS Updates
	// - recognize full service deletions which currently require special handling.
	// - to force an XDS ConfigUpdate when a DNS service endpoint is modified.
	ServiceInstancesByNamespaceHost krt.Collection[InstancesByNamespaceHost]
	// ServiceInstances is a collection of all service instances.
	// Its main purpose is to allow searching for service instances by IP.
	ServiceInstances     krt.Collection[*model.ServiceInstance]
	ServiceInstancesByIP krt.Index[string, *model.ServiceInstance]
	// Workloads is a collection of local workload instances.
	// Use cases:
	// - Notifying workload instance handlers.
	// - XDS ProxyUpdates for workload instance updates.
	Workloads krt.Collection[*model.WorkloadInstance]
}

type ServiceWithInstances struct {
	Service *model.Service
	// TODO: this is a hack to trigger full pushes when ServiceEntry target ports change, this
	// should probably be handled elsewhere.
	// ref: https://github.com/istio/istio/pull/50068
	TargetPorts []uint32
	Instances   []*model.ServiceInstance
}

func (swi ServiceWithInstances) ResourceName() string {
	return swi.Service.ResourceName()
}

func (swi ServiceWithInstances) Equals(other ServiceWithInstances) bool {
	return slices.Equal(swi.TargetPorts, other.TargetPorts) &&
		swi.Service.Equals(other.Service) &&
		slices.EqualFunc(swi.Instances, other.Instances, func(a, b *model.ServiceInstance) bool {
			return a.Endpoint.Equals(b.Endpoint)
		})
}

type InstancesByNamespaceHost struct {
	Namespace             string
	Hostname              string
	Instances             []*model.ServiceInstance
	HasDNSServiceEndpoint bool
}

func (s InstancesByNamespaceHost) ResourceName() string {
	return s.Namespace + "/" + s.Hostname
}

func (s InstancesByNamespaceHost) Equals(other InstancesByNamespaceHost) bool {
	return s.Namespace == other.Namespace && s.Hostname == other.Hostname &&
		s.HasDNSServiceEndpoint == other.HasDNSServiceEndpoint &&
		slices.EqualFunc(s.Instances, other.Instances, func(a, b *model.ServiceInstance) bool {
			return a.Equals(b)
		})
}

type Option func(*Controller)

func WithClusterID(clusterID cluster.ID) Option {
	return func(o *Controller) {
		o.clusterID = clusterID
	}
}

func WithNetworkIDCb(cb func(endpointIP string, labels labels.Instance) network.ID) Option {
	return func(o *Controller) {
		o.networkIDCallback = cb
	}
}

func WithKRTDebugger(debugger *krt.DebugHandler) Option {
	return func(o *Controller) {
		o.krtDebugger = debugger
	}
}

// NewController creates a new ServiceEntry discovery service.
func NewController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	meshConfig meshwatcher.WatcherCollection,
	options ...Option,
) *Controller {
	return newController(configController, xdsUpdater, meshConfig, false, options...)
}

// NewWorkloadEntryController creates a new WorkloadEntry discovery service.
func NewWorkloadEntryController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	meshConfig meshwatcher.WatcherCollection,
	options ...Option,
) *Controller {
	return newController(configController, xdsUpdater, meshConfig, true, options...)
}

func newController(
	store model.ConfigStoreController,
	xdsUpdater model.XDSUpdater,
	meshConfig meshwatcher.WatcherCollection,
	workloadEntryController bool,
	options ...Option,
) *Controller {
	stop := make(chan struct{})
	s := &Controller{
		workloadEntryController: workloadEntryController,
		XdsUpdater:              xdsUpdater,
		store:                   store,
		stop:                    stop,
	}
	for _, o := range options {
		o(s)
	}

	s.opts = krt.NewOptionsBuilder(stop, "serviceentry", s.krtDebugger)
	s.inputs = Inputs{
		WorkloadEntries: store.KrtCollection(gvk.WorkloadEntry),
		MeshConfig:      meshConfig.AsCollection(),
	}

	if !workloadEntryController {
		s.inputs.ServiceEntries = store.KrtCollection(gvk.ServiceEntry)
		s.inputs.ExternalWorkloads = krt.NewStaticCollection[*model.WorkloadInstance](nil, nil, s.opts.WithName("inputs/ExternalWorkloads")...)
	}

	s.buildCollections()

	if !s.workloadEntryController {
		// Register EDS/XDS push handlers
		s.handlers = append(
			s.handlers,
			s.outputs.ServiceInstancesByNamespaceHost.RegisterBatch(s.pushServiceEndpointUpdates, false),
			s.outputs.Services.RegisterBatch(s.pushServiceUpdates, false),
		)
	}
	// Register EDS/XDS push handlers for WLEs
	s.handlers = append(s.handlers, s.outputs.Workloads.RegisterBatch(s.pushWorkloadUpdates, false))

	return s
}

func (s *Controller) buildCollections() {
	wleWorkloads := krt.NewCollection(s.inputs.WorkloadEntries, func(ctx krt.HandlerContext, cfg config.Config) **model.WorkloadInstance {
		if features.WorkloadEntryHealthChecks && !isHealthy(cfg) {
			return nil
		}

		we := ConvertWorkloadEntry(cfg)
		wi := convertWorkloadEntryToWorkloadInstance(ctx, we, cfg.Meta, s.inputs.MeshConfig, cfg.Namespace, s.clusterID, s.networkIDCallback)
		return &wi
	}, s.opts.WithName("outputs/WorkloadsFromWLE")...)

	if !s.workloadEntryController {
		allWorkloads := krt.JoinCollection(
			[]krt.Collection[*model.WorkloadInstance]{wleWorkloads, s.inputs.ExternalWorkloads},
			s.opts.WithName("outputs/AllWorkloads")...,
		)
		workloadsByNamespace := krt.NewNamespaceIndex(allWorkloads)

		services, servicesByNsHost, servicesByHost := services(
			s.inputs.ServiceEntries,
			s.inputs.MeshConfig,
			allWorkloads,
			workloadsByNamespace,
			s.clusterID,
			s.networkIDCallback,
			s.opts,
		)

		mergedServicesInstances := mergeServicesInstancesByNamespaceHost(servicesByNsHost.AsCollection(), s.opts)

		// derive service instances from merged services
		serviceInstances := krt.NewManyCollection(mergedServicesInstances, func(ctx krt.HandlerContext, swi InstancesByNamespaceHost) []*model.ServiceInstance {
			return swi.Instances
		}, s.opts.WithName("outputs/ServiceInstances")...)

		serviceInstancesByIP := krt.NewIndex(serviceInstances, "ip", func(si *model.ServiceInstance) []string {
			return []string{si.Endpoint.FirstAddressOrNil()}
		})

		s.outputs = Outputs{
			Services:                        services,
			ServicesByHost:                  servicesByHost,
			ServiceInstancesByNamespaceHost: mergedServicesInstances,
			ServiceInstances:                serviceInstances,
			ServiceInstancesByIP:            serviceInstancesByIP,
		}
	}

	s.outputs.Workloads = wleWorkloads
}

func (s *Controller) pushServiceEndpointUpdates(events []krt.Event[InstancesByNamespaceHost]) {
	shard := model.ShardKeyFromRegistry(s)

	for _, e := range events {
		obj := e.Latest()
		if e.Event == controllers.EventDelete {
			// TODO: SvcUpdate should not be necessary here since EDSUpdate with no endpoints will already delete the service shard,
			// it only increments the counter and does not request a push.
			s.XdsUpdater.SvcUpdate(shard, obj.Hostname, obj.Namespace, model.EventDelete)
			s.XdsUpdater.EDSUpdate(shard, obj.Hostname, obj.Namespace, nil)
		} else {
			instances := slices.Map(obj.Instances, func(i *model.ServiceInstance) *model.IstioEndpoint {
				return i.Endpoint
			})
			s.XdsUpdater.EDSUpdate(shard, obj.Hostname, obj.Namespace, instances)
			if obj.HasDNSServiceEndpoint && e.Event == controllers.EventUpdate {
				s.XdsUpdater.ConfigUpdate(&model.PushRequest{
					Full:           true,
					ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: obj.Hostname, Namespace: obj.Namespace}),
					Reason:         model.NewReasonStats(model.EndpointUpdate),
				})
			}
		}
	}
}

func (s *Controller) pushServiceUpdates(events []krt.Event[ServiceWithInstances]) {
	configsUpdated := sets.New[model.ConfigKey]()
	shard := model.ShardKeyFromRegistry(s)
	for _, e := range events {
		if e.Event == controllers.EventUpdate &&
			e.New.Service.Equals(e.Old.Service) && slices.Equal(e.New.TargetPorts, e.Old.TargetPorts) {
			// only instances have changed
			continue
		}
		configsUpdated.Insert(makeConfigKey(e.Latest().Service))
		if e.Event != controllers.EventDelete {
			// full service deletions are not handled here
			s.XdsUpdater.SvcUpdate(shard, string(e.Latest().Service.Hostname), e.Latest().Service.Attributes.Namespace, model.Event(e.Event))
		}
	}
	if len(configsUpdated) > 0 {
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: configsUpdated,
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		})
	}
}

func (s *Controller) pushWorkloadUpdates(events []krt.Event[*model.WorkloadInstance]) {
	for _, e := range events {
		if !e.Latest().DNSServiceEntryOnly {
			s.NotifyWorkloadInstanceHandlers(e.Latest(), model.Event(e.Event))
		}

		if e.Event == controllers.EventAdd ||
			e.Event == controllers.EventUpdate && !(*e.Old).Endpoint.Labels.Equals((*e.New).Endpoint.Labels) {
			s.XdsUpdater.ProxyUpdate(s.Cluster(), (*e.New).Endpoint.FirstAddressOrNil())
		}
	}
}

func (s *Controller) NotifyWorkloadInstanceHandlers(wi *model.WorkloadInstance, event model.Event) {
	for _, h := range s.workloadHandlers {
		h(wi, event)
	}
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (s *Controller) WorkloadInstanceHandler(wi *model.WorkloadInstance, event model.Event) {
	if s.workloadEntryController {
		return
	}

	log.Debugf("Handle event %s for workload instance (%s/%v) in namespace %s", event,
		wi.Kind, wi.Endpoint.Addresses, wi.Namespace)
	// Feed external workloads collection directly
	switch event {
	case model.EventDelete:
		s.inputs.ExternalWorkloads.DeleteObject(wi.ResourceName())
	default:
		s.inputs.ExternalWorkloads.ConditionalUpdateObject(wi)
	}
}

// Run is used by some controllers to execute background jobs after init is done.
func (s *Controller) Run(stopCh <-chan struct{}) {
	<-stopCh
	close(s.stop)
}

// Services list declarations of all services in the system
func (s *Controller) Services() []*model.Service {
	if s.workloadEntryController {
		return nil
	}

	allServices := s.outputs.Services.List()
	copySvcs := make([]*model.Service, len(allServices))
	for i, svc := range allServices {
		// shallow copy, copy `AutoAllocatedIPv4Address` and `AutoAllocatedIPv6Address`
		// if return the pointer directly, there will be a race with `BuildNameTable`
		copySvcs[i] = svc.Service.ShallowCopy()
	}
	return autoAllocateIPs(copySvcs)
}

// GetService retrieves a service by host name if it exists.
// NOTE: The service entry implementation is used only for tests.
func (s *Controller) GetService(hostname host.Name) *model.Service {
	if s.workloadEntryController {
		return nil
	}

	res := s.outputs.ServicesByHost.Lookup(hostname.String())
	if len(res) == 0 {
		return nil
	}
	if len(res) == 1 {
		return res[0].Service
	}

	sortServicesByCreationTime(res)
	return res[0].Service
}

// ResyncEDS will do a full EDS update. This is needed for some tests where we have many configs loaded without calling
// the config handlers.
// This should probably not be used in production code.
func (s *Controller) ResyncEDS() {
	if s.workloadEntryController {
		return
	}

	shard := model.ShardKeyFromRegistry(s)
	for _, io := range s.outputs.ServiceInstancesByNamespaceHost.List() {
		instances := slices.Map(io.Instances, func(i *model.ServiceInstance) *model.IstioEndpoint {
			return i.Endpoint
		})
		s.XdsUpdater.EDSUpdate(shard, io.Hostname, io.Namespace, instances)
	}
}

// GetProxyServiceTargets lists service targets co-located with a given proxy
// NOTE: The service objects in these instances do not have the auto allocated IP set.
func (s *Controller) GetProxyServiceTargets(node *model.Proxy) []model.ServiceTarget {
	if s.workloadEntryController {
		return nil
	}

	out := make([]model.ServiceTarget, 0)
	for _, ip := range node.IPAddresses {
		for _, i := range s.outputs.ServiceInstancesByIP.Lookup(ip) {
			if node.Metadata.Namespace == "" || i.Service.Attributes.Namespace == node.Metadata.Namespace {
				out = append(out, model.ServiceInstanceToTarget(i))
			}
		}
	}
	return out
}

func (s *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	if s.workloadEntryController {
		return nil
	}

	for _, ip := range proxy.IPAddresses {
		for _, i := range s.outputs.ServiceInstancesByIP.Lookup(ip) {
			if proxy.Metadata.Namespace == "" || i.Service.Attributes.Namespace == proxy.Metadata.Namespace {
				return i.Endpoint.Labels
			}
		}
	}
	return nil
}

func (s *Controller) NetworkGateways() []model.NetworkGateway {
	// TODO implement mesh networks loading logic from kube controller if needed
	return nil
}

func (s *Controller) MCSServices() []model.MCSServiceInfo {
	return nil
}

func (s *Controller) Provider() provider.ID {
	return provider.External
}

func (s *Controller) Cluster() cluster.ID {
	return s.clusterID
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (s *Controller) AppendServiceHandler(_ model.ServiceHandler) {}

func (s *Controller) AppendWorkloadHandler(h func(*model.WorkloadInstance, model.Event)) {
	s.workloadHandlers = append(s.workloadHandlers, h)
}

func (s *Controller) HasSynced() bool {
	if !s.outputs.Workloads.HasSynced() {
		return false
	}

	if !s.workloadEntryController {
		if !s.outputs.Services.HasSynced() ||
			!s.outputs.ServiceInstances.HasSynced() ||
			!s.outputs.ServiceInstancesByNamespaceHost.HasSynced() {
			return false
		}
	}

	for _, h := range s.handlers {
		if !h.HasSynced() {
			return false
		}
	}

	return true
}

// similar to model.SortServicesByCreationTime but for ServiceWithInstances and with a fallback
// on Attributes.K8sAttributes.ObjectName to ensure determinism when we have multiple services
// with the same hostname in the same namespace.
func sortServicesByCreationTime(services []ServiceWithInstances) {
	slices.SortStableFunc(services, func(i, j ServiceWithInstances) int {
		if r := i.Service.CreationTime.Compare(j.Service.CreationTime); r != 0 {
			return r
		}

		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(i.Service.Attributes.Name, j.Service.Attributes.Name); r != 0 {
			return r
		}

		if r := cmp.Compare(i.Service.Attributes.Namespace, j.Service.Attributes.Namespace); r != 0 {
			return r
		}

		// Fallback on Attributes.K8sAttributes.ObjectName because Attributes.Name is actually the hostname
		// and we can have multiple services with the same hostname in the same namespace.
		return cmp.Compare(i.Service.Attributes.K8sAttributes.ObjectName, j.Service.Attributes.K8sAttributes.ObjectName)
	})
}
