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
	"strings"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
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

	multiclusterController *multicluster.Controller

	clusterID cluster.ID
	shard     model.ShardKey

	domainSuffix string

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

	canonicalServiceForMeshExternal bool

	model.NetworkGatewaysHandler
}

type Inputs struct {
	MeshConfig      krt.Collection[meshwatcher.MeshConfigResource]
	Namespaces      krt.Collection[*v1.Namespace]
	WorkloadEntries krt.Collection[config.Config]
	ServiceEntries  krt.Collection[config.Config]
	// TODO: this should be a joined collection with multi cluster workloads
	ExternalWorkloads krt.StaticCollection[*model.WorkloadInstance]
	XBackends         krt.Collection[config.Config]
}

type Outputs struct {
	// Services is a collection of unique services derived from ServiceEntries.
	// Use cases:
	// - source of truth for controller services.
	// - lookup of a service by hostname
	Services       krt.Collection[*model.Service]
	ServicesByHost krt.Index[string, *model.Service]
	// ServicesByNamespaceHost is an index collection of services keyed by namespace+hostname.
	// Use cases:
	// - drive the lifetime of merged service endpoints.
	ServicesByNamespaceHost krt.IndexCollection[string, *model.Service]
	// ServiceInstancesByNamespaceHost is a collection of instances keyed by namespace and hostname.
	// An entry exists while any service backs the key, including when it has no endpoints.
	// Use cases:
	// - as an input to EDS Updates
	ServiceInstancesByNamespaceHost krt.Collection[*InstancesByNamespaceHost]
	// ServiceInstances is a collection of all service instances.
	// Use cases:
	// - Finding service instances by IP.
	// - XDS ProxyUpdate for service instance changes.
	ServiceInstances     krt.Collection[*WorkloadServiceInstance]
	ServiceInstancesByIP krt.Index[string, *WorkloadServiceInstance]
	// Workloads is a collection of local workload instances.
	// Use cases:
	// - Notifying workload instance handlers.
	Workloads krt.Collection[*model.WorkloadInstance]
}

type ServiceWithInstances struct {
	Service   *model.Service
	Instances []*WorkloadServiceInstance
}

func (swi *ServiceWithInstances) ResourceName() string {
	return swi.Service.ResourceName()
}

func (swi *ServiceWithInstances) Equals(other *ServiceWithInstances) bool {
	if !swi.Service.Equals(other.Service) {
		return false
	}

	return slices.EqualFunc(swi.Instances, other.Instances, func(a, b *WorkloadServiceInstance) bool {
		return a.EqualsIgnoringService(b)
	})
}

type InstancesByNamespaceHost struct {
	Key       string
	Endpoints []*model.IstioEndpoint
	// HasDNSServiceEndpoint is set when an endpoint backs a DNS resolving service. Those are inlined
	// into the cluster rather than delivered over EDS, so changing them requires a config push.
	HasDNSServiceEndpoint bool
}

func (s *InstancesByNamespaceHost) ResourceName() string {
	return s.Key
}

func (s *InstancesByNamespaceHost) Equals(other *InstancesByNamespaceHost) bool {
	if s.Key != other.Key || s.HasDNSServiceEndpoint != other.HasDNSServiceEndpoint {
		return false
	}

	return slices.EqualFunc(s.Endpoints, other.Endpoints, func(a, b *model.IstioEndpoint) bool {
		return a.Equals(b)
	})
}

type WorkloadServiceInstance struct {
	UID         string
	Namespace   string
	Name        string
	Service     *model.Service       `json:"service,omitempty"`
	ServicePort *model.Port          `json:"servicePort,omitempty"`
	Endpoint    *model.IstioEndpoint `json:"endpoint,omitempty"`
}

func (wsi *WorkloadServiceInstance) ResourceName() string {
	return wsi.UID
}

// Equals checks equality of the WorkloadServiceInstance.
// Equality is determined by the UID, ServicePort, Endpoint, and Service. Namespace and Name
// are already included in the UID.
func (wsi *WorkloadServiceInstance) Equals(other *WorkloadServiceInstance) bool {
	return wsi.EqualsIgnoringService(other) &&
		wsi.Service.Equals(other.Service)
}

// EqualsIgnoringService checks equality of the WorkloadServiceInstance ignoring the Service field.
func (wsi *WorkloadServiceInstance) EqualsIgnoringService(other *WorkloadServiceInstance) bool {
	if wsi == other {
		return true
	}
	return wsi.UID == other.UID &&
		wsi.ServicePort.Equals(other.ServicePort) &&
		wsi.Endpoint.Equals(other.Endpoint)
}

type ServiceKey struct {
	Key string
}

func (s ServiceKey) ResourceName() string {
	return s.Key
}

func (s ServiceKey) Equals(other ServiceKey) bool {
	return s.Key == other.Key
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

func WithDomainSuffix(domainSuffix string) Option {
	return func(o *Controller) {
		o.domainSuffix = domainSuffix
	}
}

// NewController creates a new ServiceEntry discovery service.
func NewController(configController model.ConfigStoreController,
	xdsUpdater model.XDSUpdater,
	multiclusterController *multicluster.Controller,
	meshConfig meshwatcher.WatcherCollection,
	options ...Option,
) *Controller {
	return newController(configController, xdsUpdater, multiclusterController, meshConfig, false, options...)
}

// NewWorkloadEntryController creates a new WorkloadEntry discovery service.
func NewWorkloadEntryController(configController model.ConfigStoreController,
	xdsUpdater model.XDSUpdater,
	multiclusterController *multicluster.Controller,
	meshConfig meshwatcher.WatcherCollection,
	options ...Option,
) *Controller {
	return newController(configController, xdsUpdater, multiclusterController, meshConfig, true, options...)
}

func newController(
	store model.ConfigStoreController,
	xdsUpdater model.XDSUpdater,
	multiclusterController *multicluster.Controller,
	meshConfig meshwatcher.WatcherCollection,
	workloadEntryController bool,
	options ...Option,
) *Controller {
	stop := make(chan struct{})
	s := &Controller{
		workloadEntryController:         workloadEntryController,
		multiclusterController:          multiclusterController,
		XdsUpdater:                      xdsUpdater,
		stop:                            stop,
		canonicalServiceForMeshExternal: features.CanonicalServiceForMeshExternalServiceEntry,
	}
	for _, o := range options {
		o(s)
	}
	if s.domainSuffix == "" {
		s.domainSuffix = constants.DefaultClusterLocalDomain
	}
	s.shard = model.ShardKeyFromRegistry(s)

	s.opts = krt.NewOptionsBuilder(stop, "serviceentry", s.krtDebugger)
	s.inputs = Inputs{
		WorkloadEntries: store.KrtCollection(gvk.WorkloadEntry),
		MeshConfig:      meshConfig.AsCollection(),
	}

	if !workloadEntryController {
		s.inputs.Namespaces = multiclusterController.ConfigCluster().Namespaces()
		s.inputs.ServiceEntries = store.KrtCollection(gvk.ServiceEntry)
		s.inputs.ExternalWorkloads = krt.NewMutableCollection[*model.WorkloadInstance](nil, nil, s.opts.WithName("inputs/ExternalWorkloads")...)
		if features.EnableAlphaGatewayAPI {
			s.inputs.XBackends = store.KrtCollection(gvk.XBackend)
		}
		if s.inputs.XBackends == nil {
			s.inputs.XBackends = krt.NewStaticCollection[config.Config](nil, nil, s.opts.WithName("disable/XBackend")...)
		}
	}

	s.buildCollections(features.GlobalSendUnhealthyEndpoints.Load() || features.DefaultSendUnhealthyEndpoints.Load())

	if !s.workloadEntryController {
		// Register EDS/XDS push handlers
		s.handlers = append(
			s.handlers,
			s.outputs.ServiceInstancesByNamespaceHost.RegisterBatch(s.pushServiceEndpointUpdates, false),
			s.outputs.ServicesByNamespaceHost.RegisterBatch(s.pushServiceUpdates, false),
			s.outputs.ServiceInstances.RegisterBatch(s.pushProxyUpdates, false),
		)
	}
	s.handlers = append(s.handlers, s.outputs.Workloads.RegisterBatch(s.notifyWorkloadHandlers, false))

	return s
}

func (s *Controller) buildCollections(sendUnhealthy bool) {
	wleWorkloads := krt.NewPointerCollection(s.inputs.WorkloadEntries, func(ctx krt.HandlerContext, cfg config.Config) *model.WorkloadInstance {
		healthy := !features.WorkloadEntryHealthChecks || isHealthy(cfg)
		if !healthy && !sendUnhealthy {
			return nil
		}

		we := ConvertWorkloadEntry(cfg)
		wi := convertWorkloadEntryToWorkloadInstance(ctx, we, cfg.Meta, s.inputs.MeshConfig, cfg.Namespace, s.clusterID, s.networkIDCallback)
		wi.Endpoint.HealthStatus = model.Healthy
		if !healthy {
			wi.Endpoint.HealthStatus = model.UnHealthy
		}
		wi.Endpoint.SendUnhealthyEndpoints = sendUnhealthy
		return wi
	}, s.opts.WithName("outputs/WorkloadsFromWLE")...)

	if !s.workloadEntryController {
		backendServiceEntries := krt.NewCollection(s.inputs.XBackends, backendToServiceEntry(s.domainSuffix), s.opts.WithName("inputs/BackendServiceEntries")...)
		combinedServiceEntries := krt.JoinCollection(
			[]krt.Collection[config.Config]{s.inputs.ServiceEntries, backendServiceEntries},
			s.opts.WithName("inputs/combinedServiceEntries")...,
		)

		serviceEntryVisibility := model.ServiceEntryVisibilityCollection(s.inputs.MeshConfig, s.opts)

		servicesWithInstances := services(
			combinedServiceEntries,
			serviceEntryVisibility,
			s.inputs.MeshConfig,
			s.inputs.Namespaces,
			s.clusterID,
			s.networkIDCallback,
			s.canonicalServiceForMeshExternal,
			s.opts,
		)

		allServices := krt.MapCollection(servicesWithInstances, func(swi *ServiceWithInstances) *model.Service {
			return swi.Service
		}, append(s.opts.WithName("outputs/AllServices"), krt.WithMapDiscardEqual())...)

		allWorkloads := krt.JoinCollection(
			[]krt.Collection[*model.WorkloadInstance]{
				wleWorkloads,
				s.inputs.ExternalWorkloads.AsCollection(),
			},
			s.opts.WithName("outputs/AllWorkloads")...,
		)

		workloadServicesByNamespace := krt.NewIndex(allServices, "namespaceWithSelector", func(svc *model.Service) []string {
			if len(svc.Attributes.LabelSelectors) == 0 {
				return nil
			}
			return []string{svc.Attributes.Namespace}
		})

		servicesByHost := krt.NewIndex(allServices, "host", func(svc *model.Service) []string {
			return []string{string(svc.Hostname)}
		})
		servicesByNamespaceHost := krt.NewIndex(allServices, "namespaceHost", func(svc *model.Service) []string {
			return []string{svc.Attributes.Namespace + "/" + string(svc.Hostname)}
		}).AsCollection(s.opts.WithName("outputs/ServicesByNamespaceHost")...)

		serviceEntryInstances := krt.NewManyCollection(servicesWithInstances, func(ctx krt.HandlerContext, swi *ServiceWithInstances) []*WorkloadServiceInstance {
			// services with a workload selector have nil instances, so we only collect inline service entry instances
			return swi.Instances
		}, s.opts.WithName("outputs/ServiceEntryInstances")...)
		workloadServiceInstances := serviceInstances(allWorkloads, workloadServicesByNamespace, s.opts)
		allInstances := krt.JoinCollection([]krt.Collection[*WorkloadServiceInstance]{
			workloadServiceInstances,
			serviceEntryInstances,
		}, s.opts.WithName("outputs/AllInstances")...)

		instancesByIP := krt.NewIndex(allInstances, "ip", func(si *WorkloadServiceInstance) []string {
			return []string{si.Endpoint.FirstAddressOrNil()}
		})

		instancesByNsHost := krt.NewIndex(allInstances, "namespaceHost", func(si *WorkloadServiceInstance) []string {
			return []string{si.Service.Attributes.Namespace + "/" + string(si.Service.Hostname)}
		})

		// Index collections emit synthetic add events for updates. Materialize the keys so changes
		// to backing services only propagate when a namespace/hostname appears or disappears.
		serviceKeys := krt.NewCollection(servicesByNamespaceHost,
			func(ctx krt.HandlerContext, obj krt.IndexObject[string, *model.Service]) *ServiceKey {
				return &ServiceKey{Key: obj.Key}
			}, s.opts.WithName("outputs/ServiceKeys")...)

		mergedInstancesByNamespaceHost := krt.NewPointerCollection(
			serviceKeys,
			func(ctx krt.HandlerContext, obj ServiceKey) *InstancesByNamespaceHost {
				instances := instancesByNsHost.Fetch(ctx, obj.Key)
				if len(instances) == 0 {
					return &InstancesByNamespaceHost{
						Key:                   obj.Key,
						Endpoints:             nil,
						HasDNSServiceEndpoint: false,
					}
				}

				endpoints, hasDNSServiceEndpoint := mergeServiceInstances(instances)
				return &InstancesByNamespaceHost{
					Key:                   obj.Key,
					Endpoints:             endpoints,
					HasDNSServiceEndpoint: hasDNSServiceEndpoint,
				}
			},
			s.opts.WithName("outputs/ServiceInstancesByNamespaceHost")...,
		)

		s.outputs = Outputs{
			Services:                        allServices,
			ServicesByHost:                  servicesByHost,
			ServicesByNamespaceHost:         servicesByNamespaceHost,
			ServiceInstancesByNamespaceHost: mergedInstancesByNamespaceHost,
			ServiceInstances:                allInstances,
			ServiceInstancesByIP:            instancesByIP,
		}
	}

	s.outputs.Workloads = wleWorkloads
}

func (s *Controller) pushServiceEndpointUpdates(events []krt.Event[*InstancesByNamespaceHost]) {
	dnsServicesUpdated := sets.New[model.ConfigKey]()
	deletedServices := sets.New[model.ConfigKey]()
	for _, e := range events {
		obj := e.Latest()
		namespace, hostname, _ := strings.Cut(obj.Key, "/")
		switch {
		case e.Event == controllers.EventDelete:
			// A merged entry is deleted only when its last backing service is gone.
			s.XdsUpdater.SvcUpdate(s.shard, hostname, namespace, model.EventDelete)
			deletedServices.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: namespace})
			continue
		case e.Event == controllers.EventAdd && len(obj.Endpoints) == 0:
			// A service may appear before its instances are ready. There are no endpoints to
			// clear on an initial add; subsequent updates still publish an empty endpoint set.
			continue
		}
		s.XdsUpdater.EDSReplace(s.shard, hostname, namespace, obj.Endpoints)

		// DNS endpoints are inlined into the cluster, so gaining, changing or losing them has to regenerate it.
		needsFullPush := obj.HasDNSServiceEndpoint
		if e.Event == controllers.EventUpdate {
			needsFullPush = obj.HasDNSServiceEndpoint || (*e.Old).HasDNSServiceEndpoint
		}
		if needsFullPush {
			dnsServicesUpdated.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: namespace})
		}
	}
	if len(dnsServicesUpdated) > 0 {
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: dnsServicesUpdated,
			Reason:         model.NewReasonStats(model.EndpointUpdate),
		})
	}
	if len(deletedServices) > 0 {
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: deletedServices,
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		})
	}
}

func (s *Controller) pushServiceUpdates(events []krt.Event[krt.IndexObject[string, *model.Service]]) {
	configsUpdated := sets.New[model.ConfigKey]()
	for _, e := range events {
		// full service deletions are handled in the pushServiceEndpointUpdates handler
		if e.Event == controllers.EventDelete {
			continue
		}

		obj := e.Latest()
		namespace, hostname, _ := strings.Cut(obj.Key, "/")

		configsUpdated.Insert(model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      hostname,
			Namespace: namespace,
		})
		s.XdsUpdater.SvcUpdate(s.shard, hostname, namespace, model.Event(e.Event))
	}
	if len(configsUpdated) > 0 {
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: configsUpdated,
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		})
	}
}

func (s *Controller) notifyWorkloadHandlers(events []krt.Event[*model.WorkloadInstance]) {
	for _, e := range events {
		if !e.Latest().DNSServiceEntryOnly {
			s.NotifyWorkloadInstanceHandlers(e.Latest(), model.Event(e.Event))
		}
	}
}

type proxyKey struct {
	cluster cluster.ID
	address string
}

// pushProxyUpdates forces a workload's own proxy to recompute when this registry's instances for
// that workload change.
func (s *Controller) pushProxyUpdates(events []krt.Event[*WorkloadServiceInstance]) {
	// A workload has one instance per service port, and may be selected by several ServiceEntries;
	// collapse those into a single push per proxy.
	pushed := sets.New[proxyKey]()
	push := func(key proxyKey) {
		if key.address == "" || pushed.InsertContains(key) {
			return
		}
		s.XdsUpdater.ProxyUpdate(key.cluster, key.address)
	}
	for _, e := range events {
		si := e.Latest()
		// skip service entry inline instances
		if len(si.Service.Attributes.LabelSelectors) == 0 {
			continue
		}

		switch e.Event {
		case controllers.EventUpdate:
			// Other Service/ServicePort/Endpoint changes are pushed through other means. But the UID
			// leaves out the address, so a workload that moves to another address is an update here,
			// and its ServiceTargets move from the old proxy to the new one; both need to recompute.
			oldKey, newKey := proxyKeyForEndpoint((*e.Old).Endpoint), proxyKeyForEndpoint((*e.New).Endpoint)
			if oldKey == newKey {
				continue
			}
			push(oldKey)
			push(newKey)
		case controllers.EventDelete:
			external := s.inputs.ExternalWorkloads.GetKey(si.Namespace + "/" + si.Name)
			we := s.inputs.WorkloadEntries.GetKey(si.Namespace + "/" + si.Name)
			if external == nil && we == nil {
				// this workload no longer exists, we don't need to update any proxy
				continue
			}
			push(proxyKeyForEndpoint(si.Endpoint))
		default:
			push(proxyKeyForEndpoint(si.Endpoint))
		}
	}
}

func proxyKeyForEndpoint(ep *model.IstioEndpoint) proxyKey {
	return proxyKey{
		// ServiceEntry can select pods and WorkloadEntries from any cluster, use their own cluster ID.
		cluster: ep.Locality.ClusterID,
		address: ep.FirstAddressOrNil(),
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
	return autoAllocateIPs(allServices)
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
		return res[0]
	}

	slices.SortStableFunc(res, compareServices)
	return res[0]
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
				out = append(out, model.ServiceTarget{
					Service: i.Service,
					Port: model.ServiceInstancePort{
						ServicePort: i.ServicePort,
						TargetPort:  i.Endpoint.EndpointPort,
					},
				})
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

func compareServices(i, j *model.Service) int {
	if i == j {
		return 0
	}

	if r := i.CreationTime.Compare(j.CreationTime); r != 0 {
		return r
	}

	// If creation time is the same, then behavior is nondeterministic. In this case, we can
	// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
	// CreationTimestamp is stored in seconds, so this is not uncommon.
	if r := strings.Compare(i.Attributes.Name, j.Attributes.Name); r != 0 {
		return r
	}

	if r := strings.Compare(i.Attributes.Namespace, j.Attributes.Namespace); r != 0 {
		return r
	}

	// Fallback on Attributes.K8sAttributes.ObjectName because Attributes.Name is actually the hostname
	// and we can have multiple services with the same hostname in the same namespace.
	return strings.Compare(i.Attributes.K8sAttributes.ObjectName, j.Attributes.K8sAttributes.ObjectName)
}

// mergeServiceInstances returns the endpoints published for a host, and whether any of them backs a DNS
// resolving service.
func mergeServiceInstances(instances []*WorkloadServiceInstance) ([]*model.IstioEndpoint, bool) {
	isDNSServiceType, isDNSRoundRobin := false, false
	for _, w := range instances {
		if isDNSTypeService(w.Service) {
			isDNSServiceType = true
			if w.Service.Resolution == model.DNSRoundRobinLB {
				isDNSRoundRobin = true
				break
			}
		}
	}

	if !isDNSRoundRobin {
		// UIDs are unique and include the service, so they give a total, stable order on their own.
		slices.SortFunc(instances, func(a, b *WorkloadServiceInstance) int {
			return strings.Compare(a.UID, b.UID)
		})
		return slices.Map(instances, func(w *WorkloadServiceInstance) *model.IstioEndpoint {
			return w.Endpoint
		}), isDNSServiceType
	}

	// LOGICAL_DNS clusters accept a single endpoint, so the oldest service owns each port and a DNS
	// round robin service only publishes its first instance on a port nobody claimed before it.
	slices.SortFunc(instances, func(a, b *WorkloadServiceInstance) int {
		if r := compareServices(a.Service, b.Service); r != 0 {
			return r
		}
		return strings.Compare(a.UID, b.UID)
	})
	ports := sets.New[int]()
	res := make([]*model.IstioEndpoint, 0, len(instances))
	// Deduplication may drop every DNS endpoint, so only count the published ones.
	hasDNSServiceEndpoint := false
	for _, w := range instances {
		if w.Service.Resolution == model.DNSRoundRobinLB && ports.Contains(w.ServicePort.Port) {
			continue
		}
		ports.Insert(w.ServicePort.Port)
		res = append(res, w.Endpoint)
		hasDNSServiceEndpoint = hasDNSServiceEndpoint || isDNSTypeService(w.Service)
	}
	return res, hasDNSServiceEndpoint
}
