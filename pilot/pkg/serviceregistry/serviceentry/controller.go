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
	"fmt"
	"hash/fnv"
	"strconv"

	networking "istio.io/api/networking/v1alpha3"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
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
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

var (
	_   serviceregistry.Instance = &Controller{}
	log                          = istiolog.RegisterScope("serviceentry", "ServiceEntry registry")
)

var (
	prime  = 65011     // Used for secondary hash function.
	maxIPs = 256 * 254 // Maximum possible IPs for address allocation.
)

type octetPair struct {
	thirdOctet  int
	fourthOctet int
}

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

	// external workload handlers (unchanged API)
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
	ServiceEntries    krt.Collection[config.Config]
	WorkloadEntries   krt.Collection[config.Config]
	ExternalWorkloads krt.StaticCollection[*model.WorkloadInstance]
	MeshConfig        krt.Collection[meshwatcher.MeshConfigResource]
}

type Outputs struct {
	Services                        krt.Collection[ServiceWithInstances]
	ServicesByHost                  krt.Index[string, ServiceWithInstances]
	ServiceInstancesByNamespaceHost krt.Collection[InstancesByNamespaceHost]
	ServiceInstances                krt.Collection[*model.ServiceInstance]
	ServiceInstancesByIP            krt.Index[string, *model.ServiceInstance]
	Workloads                       krt.Collection[*model.WorkloadInstance]
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
	return swi.Service.Equals(other.Service) &&
		slices.Equal(swi.TargetPorts, other.TargetPorts) &&
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
	return s.HasDNSServiceEndpoint == other.HasDNSServiceEndpoint && slices.EqualFunc(s.Instances, other.Instances, func(a, b *model.ServiceInstance) bool {
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

	// Build derived collections
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
		workloads := krt.JoinCollection(
			[]krt.Collection[*model.WorkloadInstance]{wleWorkloads, s.inputs.ExternalWorkloads},
			s.opts.WithName("outputs/AllWorkloads")...,
		)
		workloadsByNamespace := krt.NewIndex(workloads, "byNamespace", func(wi *model.WorkloadInstance) []string {
			return []string{wi.Namespace}
		})

		services, servicesByNsHost, servicesByHost := services(
			s.inputs.ServiceEntries,
			s.inputs.MeshConfig,
			workloads,
			workloadsByNamespace,
			s.clusterID,
			s.networkIDCallback,
			s.opts,
		)

		mergedServices := mergeServicesByNamespaceHost(servicesByNsHost.AsCollection(), s.opts)

		// derive service instances from merged services
		serviceInstances := krt.NewManyCollection(mergedServices, func(ctx krt.HandlerContext, swi InstancesByNamespaceHost) []*model.ServiceInstance {
			return swi.Instances
		}, s.opts.WithName("outputs/ServiceInstances")...)

		serviceInstancesByIP := krt.NewIndex(serviceInstances, "byIP", func(si *model.ServiceInstance) []string {
			return []string{si.Endpoint.FirstAddressOrNil()}
		})

		s.outputs = Outputs{
			Services:                        services,
			ServicesByHost:                  servicesByHost,
			ServiceInstancesByNamespaceHost: mergedServices,
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

func (s *Controller) pushServiceUpdates(events []krt.Event[ServiceWithInstances]) {
	cu := sets.New[model.ConfigKey]()
	shard := model.ShardKeyFromRegistry(s)
	for _, e := range events {
		if e.Event == controllers.EventUpdate &&
			e.New.Service.Equals(e.Old.Service) && slices.Equal(e.New.TargetPorts, e.Old.TargetPorts) {
			// only instances have changed
			continue
		}
		cu.Insert(makeConfigKey(e.Latest().Service))
		if e.Event != controllers.EventDelete {
			// full service deletions are not handled here
			s.XdsUpdater.SvcUpdate(shard, string(e.Latest().Service.Hostname), e.Latest().Service.Attributes.Namespace, model.Event(e.Event))
		}
	}
	if len(cu) > 0 {
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		})
	}
}

// ConvertClientWorkloadEntry merges the metadata.labels and spec.labels
func ConvertClientWorkloadEntry(cfg *clientnetworking.WorkloadEntry) *clientnetworking.WorkloadEntry {
	if cfg.Spec.Labels == nil {
		// Short circuit, we don't have to do any conversion
		return cfg
	}
	cfg = cfg.DeepCopy()
	// Set both fields to be the merged result, so either can be used
	cfg.Spec.Labels = maps.MergeCopy(cfg.Spec.Labels, cfg.Labels)
	cfg.Labels = cfg.Spec.Labels

	return cfg
}

// ConvertWorkloadEntry convert wle from Config.Spec and populate the metadata labels into it.
func ConvertWorkloadEntry(cfg config.Config) *networking.WorkloadEntry {
	wle := cfg.Spec.(*networking.WorkloadEntry)
	if wle == nil {
		return nil
	}

	// we will merge labels from metadata with spec, with precedence to the metadata
	labels := maps.MergeCopy(wle.Labels, cfg.Labels)
	// shallow copy
	copied := protomarshal.ShallowClone(wle)
	copied.Labels = labels
	return copied
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
		s.inputs.ExternalWorkloads.UpdateObject(wi)
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

// Automatically allocates IPs for service entry services WITHOUT an
// address field if the hostname is not a wildcard, or when resolution
// is not NONE. The IPs are allocated from the reserved Class E subnet
// (240.240.0.0/16) that is not reachable outside the pod or reserved
// Benchmarking IP range (2001:2::/48) in RFC5180. When DNS
// capture is enabled, Envoy will resolve the DNS to these IPs. The
// listeners for TCP services will also be set up on these IPs. The
// IPs allocated to a service entry may differ from istiod to istiod
// but it does not matter because these IPs only affect the listener
// IPs on a given proxy managed by a given istiod.
//
// NOTE: If DNS capture is not enabled by the proxy, the automatically
// allocated IP addresses do not take effect.
//
// The current algorithm to allocate IPs is deterministic across all istiods.
func autoAllocateIPs(services []*model.Service) []*model.Service {
	// if we are using the IP Autoallocate controller then we can short circuit this
	if features.EnableIPAutoallocate {
		return services
	}
	hashedServices := make([]*model.Service, maxIPs)
	hash := fnv.New32a()
	// First iterate through the range of services and determine its position by hash
	// so that we can deterministically allocate an IP.
	// We use "Double Hashning" for collision detection.
	// The hash algorithm is
	// - h1(k) = Sum32 hash of the service key (namespace + "/" + hostname)
	// - Check if we have an empty slot for h1(x) % MAXIPS. Use it if available.
	// - If there is a collision, apply second hash i.e. h2(x) = PRIME - (Key % PRIME)
	//   where PRIME is the max prime number below MAXIPS.
	// - Calculate new hash iteratively till we find an empty slot with (h1(k) + i*h2(k)) % MAXIPS
	j := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() && svc.Resolution != model.Passthrough {
			if j >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries. maxips:= %d", maxIPs)
				break
			}
			// First hash is calculated by hashing the service key i.e. (namespace + "/" + hostname).
			hash.Write([]byte(svc.Key()))
			s := hash.Sum32()
			firstHash := s % uint32(maxIPs)
			// Check if there is a service with this hash first. If there is no service
			// at this location - then we can safely assign this position for this service.
			if hashedServices[firstHash] == nil {
				hashedServices[firstHash] = svc
			} else {
				// This means we have a collision. Resolve collision by "DoubleHashing".
				i := uint32(1)
				secondHash := uint32(prime) - (s % uint32(prime))
				for {
					nh := (s + i*secondHash) % uint32(maxIPs-1)
					if hashedServices[nh] == nil {
						hashedServices[nh] = svc
						break
					}
					i++
				}
			}
			hash.Reset()
			j++
		}
	}

	x := 0
	hnMap := make(map[string]octetPair)
	for _, svc := range hashedServices {
		x++
		if svc == nil {
			// There is no service in the slot. Just increment x and move forward.
			continue
		}
		n := svc.Key()
		// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
		// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
		// So we bump X to 511, so that the resulting IP is 240.240.2.1
		if x%255 == 0 {
			x++
		}
		if v, ok := hnMap[n]; ok {
			log.Debugf("Reuse IP for domain %s", n)
			setAutoAllocatedIPs(svc, v)
		} else {
			thirdOctect := x / 255
			fourthOctect := x % 255
			pair := octetPair{thirdOctect, fourthOctect}
			setAutoAllocatedIPs(svc, pair)
			hnMap[n] = pair
		}
	}
	return services
}

func setAutoAllocatedIPs(svc *model.Service, octets octetPair) {
	a := octets.thirdOctet
	b := octets.fourthOctet
	svc.AutoAllocatedIPv4Address = fmt.Sprintf("240.240.%d.%d", a, b)
	if a == 0 {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x", b)
	} else {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x%x", a, b)
	}
}

func makeConfigKey(svc *model.Service) model.ConfigKey {
	return model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(svc.Hostname),
		Namespace: svc.Attributes.Namespace,
	}
}

// isHealthy checks that the provided WorkloadEntry is healthy. If health checks are not enabled,
// it is assumed to always be healthy
func isHealthy(cfg config.Config) bool {
	if parseHealthAnnotation(cfg.Annotations[status.WorkloadEntryHealthCheckAnnotation]) {
		// We default to false if the condition is not set. This ensures newly created WorkloadEntries
		// are treated as unhealthy until we prove they are healthy by probe success.
		return status.GetBoolConditionFromSpec(cfg, status.ConditionHealthy, false)
	}
	// If health check is not enabled, assume its healthy
	return true
}

func parseHealthAnnotation(s string) bool {
	if s == "" {
		return false
	}
	p, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return p
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
	if s.workloadEntryController {
		if !s.inputs.WorkloadEntries.HasSynced() {
			return false
		}
	} else {
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

func sortServicesByCreationTime(services []ServiceWithInstances) {
	slices.SortStableFunc(services, func(i, j ServiceWithInstances) int {
		if r := i.Service.CreationTime.Compare(j.Service.CreationTime); r != 0 {
			return r
		}

		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(i.Service.Attributes.K8sAttributes.ObjectName, j.Service.Attributes.K8sAttributes.ObjectName); r != 0 {
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
