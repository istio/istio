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

package controller

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/util/workloadinstances"
	"istio.io/istio/pilot/pkg/util/informermetric"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/queue"
	istiolog "istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

const (
	// NodeRegionLabel is the well-known label for kubernetes node region in beta
	NodeRegionLabel = v1.LabelFailureDomainBetaRegion
	// NodeZoneLabel is the well-known label for kubernetes node zone in beta
	NodeZoneLabel = v1.LabelFailureDomainBetaZone
	// NodeRegionLabelGA is the well-known label for kubernetes node region in ga
	NodeRegionLabelGA = v1.LabelTopologyRegion
	// NodeZoneLabelGA is the well-known label for kubernetes node zone in ga
	NodeZoneLabelGA = v1.LabelTopologyZone

	// DefaultNetworkGatewayPort is the port used by default for cross-network traffic if not otherwise specified
	// by meshNetworks or "networking.istio.io/gatewayPort"
	DefaultNetworkGatewayPort = 15443
)

var log = istiolog.RegisterScope("kube", "kubernetes service registry controller", 0)

var (
	typeTag  = monitoring.MustCreateLabel("type")
	eventTag = monitoring.MustCreateLabel("event")

	k8sEvents = monitoring.NewSum(
		"pilot_k8s_reg_events",
		"Events from k8s registry.",
		monitoring.WithLabels(typeTag, eventTag),
	)

	// nolint: gocritic
	// This is deprecated in favor of `pilot_k8s_endpoints_pending_pod`, which is a gauge indicating the number of
	// currently missing pods. This helps distinguish transient errors from permanent ones
	endpointsWithNoPods = monitoring.NewSum(
		"pilot_k8s_endpoints_with_no_pods",
		"Endpoints that does not have any corresponding pods.")

	endpointsPendingPodUpdate = monitoring.NewGauge(
		"pilot_k8s_endpoints_pending_pod",
		"Number of endpoints that do not currently have any corresponding pods.",
	)
)

func init() {
	monitoring.MustRegister(k8sEvents)
	monitoring.MustRegister(endpointsWithNoPods)
	monitoring.MustRegister(endpointsPendingPodUpdate)
}

func incrementEvent(kind, event string) {
	k8sEvents.With(typeTag.Value(kind), eventTag.Value(event)).Increment()
}

// Options stores the configurable attributes of a Controller.
type Options struct {
	SystemNamespace string

	// MeshServiceController is a mesh-wide service Controller.
	MeshServiceController *aggregate.Controller

	DomainSuffix string

	// ClusterID identifies the remote cluster in a multicluster env.
	ClusterID cluster.ID

	// ClusterAliases are aliase names for cluster. When a proxy connects with a cluster ID
	// and if it has a different alias we should use that a cluster ID for proxy.
	ClusterAliases map[string]string

	// Metrics for capturing node-based metrics.
	Metrics model.Metrics

	// XDSUpdater will push changes to the xDS server.
	XDSUpdater model.XDSUpdater

	// NetworksWatcher observes changes to the mesh networks config.
	NetworksWatcher mesh.NetworksWatcher

	// MeshWatcher observes changes to the mesh config
	MeshWatcher mesh.Watcher

	// EndpointMode decides what source to use to get endpoint information
	EndpointMode EndpointMode

	// Maximum QPS when communicating with kubernetes API
	KubernetesAPIQPS float32

	// Maximum burst for throttle when communicating with the kubernetes API
	KubernetesAPIBurst int

	// SyncTimeout, if set, causes HasSynced to be returned when timeout.
	SyncTimeout time.Duration

	// If meshConfig.DiscoverySelectors are specified, the DiscoveryNamespacesFilter tracks the namespaces this controller watches.
	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
}

// DetectEndpointMode determines whether to use Endpoints or EndpointSlice based on the
// feature flag and/or Kubernetes version
func DetectEndpointMode(kubeClient kubelib.Client) EndpointMode {
	useEndpointslice, ok := features.EnableEndpointSliceController()

	// we have a client, and flag wasn't set explicitly, auto-detect
	if kubeClient != nil && !ok && kubelib.IsAtLeastVersion(kubeClient, 21) {
		useEndpointslice = true
	}

	if useEndpointslice {
		return EndpointSliceOnly
	}
	return EndpointsOnly
}

// EndpointMode decides what source to use to get endpoint information
type EndpointMode int

const (
	// EndpointsOnly type will use only Kubernetes Endpoints
	EndpointsOnly EndpointMode = iota

	// EndpointSliceOnly type will use only Kubernetes EndpointSlices
	EndpointSliceOnly

	// TODO: add other modes. Likely want a mode with Endpoints+EndpointSlices that are not controlled by
	// Kubernetes Controller (e.g. made by user and not duplicated with Endpoints), or a mode with both that
	// does deduping. Simply doing both won't work for now, since not all Kubernetes components support EndpointSlice.
)

var EndpointModes = []EndpointMode{EndpointsOnly, EndpointSliceOnly}

var EndpointModeNames = map[EndpointMode]string{
	EndpointsOnly:     "EndpointsOnly",
	EndpointSliceOnly: "EndpointSliceOnly",
}

func (m EndpointMode) String() string {
	return EndpointModeNames[m]
}

// kubernetesNode represents a kubernetes node that is reachable externally
type kubernetesNode struct {
	address string
	labels  labels.Instance
}

// controllerInterface is a simplified interface for the Controller used for testing.
type controllerInterface interface {
	getPodLocality(pod *v1.Pod) string
	Network(endpointIP string, labels labels.Instance) network.ID
	Cluster() cluster.ID
}

var (
	_ controllerInterface      = &Controller{}
	_ serviceregistry.Instance = &Controller{}
)

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	opts Options

	client kubelib.Client

	queue queue.Instance

	nsInformer cache.SharedIndexInformer
	nsLister   listerv1.NamespaceLister

	serviceInformer filter.FilteredSharedIndexInformer
	serviceLister   listerv1.ServiceLister

	endpoints kubeEndpointsController

	// Used to watch node accessible from remote cluster.
	// In multi-cluster(shared control plane multi-networks) scenario, ingress gateway service can be of nodePort type.
	// With this, we can populate mesh's gateway address with the node ips.
	nodeInformer cache.SharedIndexInformer
	nodeLister   listerv1.NodeLister

	exports serviceExportCache
	imports serviceImportCache
	pods    *PodCache

	handlers model.ControllerHandlers

	// This is only used for test
	stop chan struct{}

	sync.RWMutex
	// servicesMap stores hostname ==> service, it is used to reduce convertService calls.
	servicesMap map[host.Name]*model.Service
	// hostNamesForNamespacedName returns all possible hostnames for the given service name.
	// If Kubernetes Multi-Cluster Services (MCS) is enabled, this will contain the regular
	// hostname as well as the MCS hostname (clusterset.local). Otherwise, only the regular
	// hostname will be returned.
	hostNamesForNamespacedName func(name types.NamespacedName) []host.Name
	// servicesForNamespacedName returns all services for the given service name.
	// If Kubernetes Multi-Cluster Services (MCS) is enabled, this will contain the regular
	// service as well as the MCS service (clusterset.local), if available. Otherwise,
	// only the regular service will be returned.
	servicesForNamespacedName func(name types.NamespacedName) []*model.Service
	// nodeSelectorsForServices stores hostname => label selectors that can be used to
	// refine the set of node port IPs for a service.
	nodeSelectorsForServices map[host.Name]labels.Instance
	// map of node name and its address+labels - this is the only thing we need from nodes
	// for vm to k8s or cross cluster. When node port services select specific nodes by labels,
	// we run through the label selectors here to pick only ones that we need.
	// Only nodes with ExternalIP addresses are included in this map !
	nodeInfoMap map[string]kubernetesNode
	// externalNameSvcInstanceMap stores hostname ==> instance, is used to store instances for ExternalName k8s services
	externalNameSvcInstanceMap map[host.Name][]*model.ServiceInstance
	// index over workload instances from workload entries
	workloadInstancesIndex workloadinstances.Index

	multinetwork
	// informerInit is set to true once the controller is running successfully. This ensures we do not
	// return HasSynced=true before we are running
	informerInit *atomic.Bool
	// beginSync is set to true when calling SyncAll, it indicates the controller has began sync resources.
	beginSync *atomic.Bool
	// initialSync is set to true after performing an initial in-order processing of all objects.
	initialSync *atomic.Bool
}

// NewController creates a new Kubernetes controller
// Created by bootstrap and multicluster (see multicluster.Controller).
func NewController(kubeClient kubelib.Client, options Options) *Controller {
	c := &Controller{
		opts:                       options,
		client:                     kubeClient,
		queue:                      queue.NewQueueWithID(1*time.Second, string(options.ClusterID)),
		servicesMap:                make(map[host.Name]*model.Service),
		nodeSelectorsForServices:   make(map[host.Name]labels.Instance),
		nodeInfoMap:                make(map[string]kubernetesNode),
		externalNameSvcInstanceMap: make(map[host.Name][]*model.ServiceInstance),
		workloadInstancesIndex:     workloadinstances.NewIndex(),
		informerInit:               atomic.NewBool(false),
		beginSync:                  atomic.NewBool(false),
		initialSync:                atomic.NewBool(false),

		multinetwork: initMultinetwork(),
	}

	if features.EnableMCSHost {
		c.hostNamesForNamespacedName = func(name types.NamespacedName) []host.Name {
			return []host.Name{
				kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix),
				serviceClusterSetLocalHostname(name),
			}
		}
		c.servicesForNamespacedName = func(name types.NamespacedName) []*model.Service {
			out := make([]*model.Service, 0, 2)

			c.RLock()
			if svc := c.servicesMap[kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix)]; svc != nil {
				out = append(out, svc)
			}

			if svc := c.servicesMap[serviceClusterSetLocalHostname(name)]; svc != nil {
				out = append(out, svc)
			}
			c.RUnlock()

			return out
		}
	} else {
		c.hostNamesForNamespacedName = func(name types.NamespacedName) []host.Name {
			return []host.Name{
				kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix),
			}
		}
		c.servicesForNamespacedName = func(name types.NamespacedName) []*model.Service {
			if svc := c.GetService(kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix)); svc != nil {
				return []*model.Service{svc}
			}
			return nil
		}
	}

	c.nsInformer = kubeClient.KubeInformer().Core().V1().Namespaces().Informer()
	c.nsLister = kubeClient.KubeInformer().Core().V1().Namespaces().Lister()
	if c.opts.SystemNamespace != "" {
		nsInformer := filter.NewFilteredSharedIndexInformer(func(obj interface{}) bool {
			ns, ok := obj.(*v1.Namespace)
			if !ok {
				log.Warnf("Namespace watch getting wrong type in event: %T", obj)
				return false
			}
			return ns.Name == c.opts.SystemNamespace
		}, c.nsInformer)
		c.registerHandlers(nsInformer, "Namespaces", c.onSystemNamespaceEvent, nil)
	}

	if c.opts.DiscoveryNamespacesFilter == nil {
		c.opts.DiscoveryNamespacesFilter = filter.NewDiscoveryNamespacesFilter(c.nsLister, options.MeshWatcher.Mesh().DiscoverySelectors)
	}

	c.initDiscoveryHandlers(kubeClient, options.EndpointMode, options.MeshWatcher, c.opts.DiscoveryNamespacesFilter)

	c.serviceInformer = filter.NewFilteredSharedIndexInformer(c.opts.DiscoveryNamespacesFilter.Filter, kubeClient.KubeInformer().Core().V1().Services().Informer())
	c.serviceLister = listerv1.NewServiceLister(c.serviceInformer.GetIndexer())

	c.registerHandlers(c.serviceInformer, "Services", c.onServiceEvent, nil)

	switch options.EndpointMode {
	case EndpointsOnly:
		c.endpoints = newEndpointsController(c)
	case EndpointSliceOnly:
		c.endpoints = newEndpointSliceController(c)
	}

	// This is for getting the node IPs of a selected set of nodes
	c.nodeInformer = kubeClient.KubeInformer().Core().V1().Nodes().Informer()
	c.nodeLister = kubeClient.KubeInformer().Core().V1().Nodes().Lister()
	c.registerHandlers(c.nodeInformer, "Nodes", c.onNodeEvent, nil)

	podInformer := filter.NewFilteredSharedIndexInformer(c.opts.DiscoveryNamespacesFilter.Filter, kubeClient.KubeInformer().Core().V1().Pods().Informer())
	c.pods = newPodCache(c, podInformer, func(key string) {
		item, exists, err := c.endpoints.getInformer().GetIndexer().GetByKey(key)
		if err != nil {
			log.Debugf("Endpoint %v lookup failed with error %v, skipping stale endpoint", key, err)
			return
		}
		if !exists {
			log.Debugf("Endpoint %v not found, skipping stale endpoint", key)
			return
		}
		if shouldEnqueue("Pods", c.beginSync) {
			c.queue.Push(func() error {
				return c.endpoints.onEvent(item, model.EventUpdate)
			})
		}
	})
	c.registerHandlers(c.pods.informer, "Pods", c.pods.onEvent, nil)

	c.exports = newServiceExportCache(c)
	c.imports = newServiceImportCache(c)

	return c
}

func (c *Controller) Provider() provider.ID {
	return provider.Kubernetes
}

func (c *Controller) Cluster() cluster.ID {
	return c.opts.ClusterID
}

func (c *Controller) MCSServices() []model.MCSServiceInfo {
	outMap := make(map[types.NamespacedName]*model.MCSServiceInfo)

	// Add the ServiceExport info.
	for _, se := range c.exports.ExportedServices() {
		mcsService := outMap[se.namespacedName]
		if mcsService == nil {
			mcsService = &model.MCSServiceInfo{}
			outMap[se.namespacedName] = mcsService
		}
		mcsService.Cluster = c.Cluster()
		mcsService.Name = se.namespacedName.Name
		mcsService.Namespace = se.namespacedName.Namespace
		mcsService.Exported = true
		mcsService.Discoverability = se.discoverability
	}

	// Add the ServiceImport info.
	for _, si := range c.imports.ImportedServices() {
		mcsService := outMap[si.namespacedName]
		if mcsService == nil {
			mcsService = &model.MCSServiceInfo{}
			outMap[si.namespacedName] = mcsService
		}
		mcsService.Cluster = c.Cluster()
		mcsService.Name = si.namespacedName.Name
		mcsService.Namespace = si.namespacedName.Namespace
		mcsService.Imported = true
		mcsService.ClusterSetVIP = si.clusterSetVIP
	}

	out := make([]model.MCSServiceInfo, 0, len(outMap))
	for _, v := range outMap {
		out = append(out, *v)
	}

	return out
}

func (c *Controller) networkFromMeshNetworks(endpointIP string) network.ID {
	c.RLock()
	defer c.RUnlock()
	if c.networkForRegistry != "" {
		return c.networkForRegistry
	}

	if c.ranger != nil {
		entries, err := c.ranger.ContainingNetworks(net.ParseIP(endpointIP))
		if err != nil {
			log.Error(err)
			return ""
		}
		if len(entries) > 1 {
			log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
		}
		if len(entries) > 0 {
			return (entries[0].(namedRangerEntry)).name
		}
	}
	return ""
}

func (c *Controller) networkFromSystemNamespace() network.ID {
	c.RLock()
	defer c.RUnlock()
	return c.network
}

func (c *Controller) Network(endpointIP string, labels labels.Instance) network.ID {
	// 1. check the pod/workloadEntry label
	if nw := labels[label.TopologyNetwork.Name]; nw != "" {
		return network.ID(nw)
	}

	// 2. check the system namespace labels
	if nw := c.networkFromSystemNamespace(); nw != "" {
		return nw
	}

	// 3. check the meshNetworks config
	if nw := c.networkFromMeshNetworks(endpointIP); nw != "" {
		return nw
	}

	return ""
}

func (c *Controller) Cleanup() error {
	if err := queue.WaitForClose(c.queue, 30*time.Second); err != nil {
		log.Warnf("queue for removed kube registry %q may not be done processing: %v", c.Cluster(), err)
	}
	if c.opts.XDSUpdater != nil {
		c.opts.XDSUpdater.RemoveShard(model.ShardKeyFromRegistry(c))
	}
	return nil
}

func (c *Controller) onServiceEvent(curr interface{}, event model.Event) error {
	svc, err := convertToService(curr)
	if err != nil {
		log.Errorf(err)
		return nil
	}

	log.Debugf("Handle event %s for service %s in namespace %s", event, svc.Name, svc.Namespace)

	// Create the standard (cluster.local) service.
	svcConv := kube.ConvertService(*svc, c.opts.DomainSuffix, c.Cluster())
	switch event {
	case model.EventDelete:
		c.deleteService(svcConv)
	default:
		c.addOrUpdateService(svc, svcConv, event, false)
	}

	return nil
}

func (c *Controller) deleteService(svc *model.Service) {
	c.Lock()
	delete(c.servicesMap, svc.Hostname)
	delete(c.nodeSelectorsForServices, svc.Hostname)
	delete(c.externalNameSvcInstanceMap, svc.Hostname)
	_, isNetworkGateway := c.networkGatewaysBySvc[svc.Hostname]
	delete(c.networkGatewaysBySvc, svc.Hostname)
	c.Unlock()

	if isNetworkGateway {
		c.NotifyGatewayHandlers()
		// TODO trigger push via handler
		// networks are different, we need to update all eds endpoints
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.NetworksTrigger}})
	}

	shard := model.ShardKeyFromRegistry(c)
	event := model.EventDelete
	c.opts.XDSUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, event)

	c.handlers.NotifyServiceHandlers(svc, event)
}

func (c *Controller) addOrUpdateService(svc *v1.Service, svcConv *model.Service, event model.Event, updateEDSCache bool) {
	needsFullPush := false
	// First, process nodePort gateway service, whose externalIPs specified
	// and loadbalancer gateway service
	if !svcConv.Attributes.ClusterExternalAddresses.IsEmpty() {
		needsFullPush = c.extractGatewaysFromService(svcConv)
	} else if isNodePortGatewayService(svc) {
		// We need to know which services are using node selectors because during node events,
		// we have to update all the node port services accordingly.
		nodeSelector := getNodeSelectorsForService(svc)
		c.Lock()
		// only add when it is nodePort gateway service
		c.nodeSelectorsForServices[svcConv.Hostname] = nodeSelector
		c.Unlock()
		needsFullPush = c.updateServiceNodePortAddresses(svcConv)
	}

	// instance conversion is only required when service is added/updated.
	instances := kube.ExternalNameServiceInstances(svc, svcConv)
	c.Lock()
	c.servicesMap[svcConv.Hostname] = svcConv
	if len(instances) > 0 {
		c.externalNameSvcInstanceMap[svcConv.Hostname] = instances
	}
	c.Unlock()

	if needsFullPush {
		// networks are different, we need to update all eds endpoints
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.NetworksTrigger}})
	}

	shard := model.ShardKeyFromRegistry(c)
	ns := svcConv.Attributes.Namespace
	// We also need to update when the Service changes. For Kubernetes, a service change will result in Endpoint updates,
	// but workload entries will also need to be updated.
	// TODO(nmittler): Build different sets of endpoints for cluster.local and clusterset.local.
	endpoints := c.buildEndpointsForService(svcConv, updateEDSCache)
	if len(endpoints) > 0 {
		c.opts.XDSUpdater.EDSCacheUpdate(shard, string(svcConv.Hostname), ns, endpoints)
	}

	c.opts.XDSUpdater.SvcUpdate(shard, string(svcConv.Hostname), ns, event)

	c.handlers.NotifyServiceHandlers(svcConv, event)
}

func (c *Controller) buildEndpointsForService(svc *model.Service, updateCache bool) []*model.IstioEndpoint {
	endpoints := c.endpoints.buildIstioEndpointsWithService(svc.Attributes.Name, svc.Attributes.Namespace, svc.Hostname, updateCache)
	fep := c.collectWorkloadInstanceEndpoints(svc)
	endpoints = append(endpoints, fep...)
	return endpoints
}

func (c *Controller) onNodeEvent(obj interface{}, event model.Event) error {
	node, ok := obj.(*v1.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("couldn't get object from tombstone %+v", obj)
			return nil
		}
		node, ok = tombstone.Obj.(*v1.Node)
		if !ok {
			log.Errorf("tombstone contained object that is not a node %#v", obj)
			return nil
		}
	}
	var updatedNeeded bool
	if event == model.EventDelete {
		updatedNeeded = true
		c.Lock()
		delete(c.nodeInfoMap, node.Name)
		c.Unlock()
	} else {
		k8sNode := kubernetesNode{labels: node.Labels}
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeExternalIP && address.Address != "" {
				k8sNode.address = address.Address
				break
			}
		}
		if k8sNode.address == "" {
			return nil
		}

		c.Lock()
		// check if the node exists as this add event could be due to controller resync
		// if the stored object changes, then fire an update event. Otherwise, ignore this event.
		currentNode, exists := c.nodeInfoMap[node.Name]
		if !exists || !nodeEquals(currentNode, k8sNode) {
			c.nodeInfoMap[node.Name] = k8sNode
			updatedNeeded = true
		}
		c.Unlock()
	}

	// update all related services
	if updatedNeeded && c.updateServiceNodePortAddresses() {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: []model.TriggerReason{model.ServiceUpdate},
		})
	}
	return nil
}

// FilterOutFunc func for filtering out objects during update callback
type FilterOutFunc func(old, cur interface{}) bool

func (c *Controller) registerHandlers(
	informer filter.FilteredSharedIndexInformer, otype string,
	handler func(interface{}, model.Event) error, filter FilterOutFunc,
) {
	wrappedHandler := func(obj interface{}, event model.Event) error {
		obj = tryGetLatestObject(informer, obj)
		return handler(obj, event)
	}
	if informer, ok := informer.(cache.SharedInformer); ok {
		_ = informer.SetWatchErrorHandler(informermetric.ErrorHandlerForCluster(c.Cluster()))
	}
	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				incrementEvent(otype, "add")
				if !shouldEnqueue(otype, c.beginSync) {
					return
				}
				c.queue.Push(func() error {
					return wrappedHandler(obj, model.EventAdd)
				})
			},
			UpdateFunc: func(old, cur interface{}) {
				if filter != nil {
					if filter(old, cur) {
						incrementEvent(otype, "updatesame")
						return
					}
				}

				incrementEvent(otype, "update")
				if !shouldEnqueue(otype, c.beginSync) {
					return
				}
				c.queue.Push(func() error {
					return wrappedHandler(cur, model.EventUpdate)
				})
			},
			DeleteFunc: func(obj interface{}) {
				incrementEvent(otype, "delete")
				if !shouldEnqueue(otype, c.beginSync) {
					return
				}
				c.queue.Push(func() error {
					return handler(obj, model.EventDelete)
				})
			},
		})
}

// tryGetLatestObject attempts to fetch the latest version of the object from the cache.
// Changes may have occurred between queuing and processing.
func tryGetLatestObject(informer filter.FilteredSharedIndexInformer, obj interface{}) interface{} {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Warnf("failed creating key for informer object: %v", err)
		return obj
	}

	latest, exists, err := informer.GetIndexer().GetByKey(key)
	if !exists || err != nil {
		log.Warnf("couldn't find %q in informer index", key)
		return obj
	}

	return latest
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	return c.initialSync.Load()
}

func (c *Controller) informersSynced() bool {
	if !c.informerInit.Load() {
		// registration/Run of informers hasn't occurred yet
		return false
	}
	if (c.nsInformer != nil && !c.nsInformer.HasSynced()) ||
		!c.serviceInformer.HasSynced() ||
		!c.endpoints.HasSynced() ||
		!c.pods.informer.HasSynced() ||
		!c.nodeInformer.HasSynced() ||
		!c.exports.HasSynced() ||
		!c.imports.HasSynced() {
		return false
	}
	return true
}

// SyncAll syncs all the objects node->service->pod->endpoint in order
// TODO: sync same kind of objects in parallel
// This can cause great performance cost in multi clusters scenario.
// Maybe just sync the cache and trigger one push at last.
func (c *Controller) SyncAll() error {
	c.beginSync.Store(true)
	var err *multierror.Error
	err = multierror.Append(err, c.syncDiscoveryNamespaces())
	err = multierror.Append(err, c.syncSystemNamespace())
	err = multierror.Append(err, c.syncNodes())
	err = multierror.Append(err, c.syncServices())
	err = multierror.Append(err, c.syncPods())
	err = multierror.Append(err, c.syncEndpoints())

	return multierror.Flatten(err.ErrorOrNil())
}

func (c *Controller) syncSystemNamespace() error {
	var err error
	if c.nsLister != nil {
		sysNs, _ := c.nsLister.Get(c.opts.SystemNamespace)
		log.Debugf("initializing systemNamespace:%s", c.opts.SystemNamespace)
		if sysNs != nil {
			err = c.onSystemNamespaceEvent(sysNs, model.EventAdd)
		}
	}
	return err
}

func (c *Controller) syncDiscoveryNamespaces() error {
	var err error
	if c.nsLister != nil {
		err = c.opts.DiscoveryNamespacesFilter.SyncNamespaces()
	}
	return err
}

func (c *Controller) syncNodes() error {
	var err *multierror.Error
	nodes := c.nodeInformer.GetIndexer().List()
	log.Debugf("initializing %d nodes", len(nodes))
	for _, s := range nodes {
		err = multierror.Append(err, c.onNodeEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

func (c *Controller) syncServices() error {
	var err *multierror.Error
	services := c.serviceInformer.GetIndexer().List()
	log.Debugf("initializing %d services", len(services))
	for _, s := range services {
		err = multierror.Append(err, c.onServiceEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

func (c *Controller) syncPods() error {
	var err *multierror.Error
	pods := c.pods.informer.GetIndexer().List()
	log.Debugf("initializing %d pods", len(pods))
	for _, s := range pods {
		err = multierror.Append(err, c.pods.onEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

func (c *Controller) syncEndpoints() error {
	var err *multierror.Error
	endpoints := c.endpoints.getInformer().GetIndexer().List()
	log.Debugf("initializing %d endpoints", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, c.endpoints.onEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	if c.opts.SyncTimeout != 0 {
		time.AfterFunc(c.opts.SyncTimeout, func() {
			if !c.informerInit.Load() {
				log.Warnf("kube controller for %s initial sync timed out", c.opts.ClusterID)
				c.informerInit.Store(true)
			}
		})
	}
	st := time.Now()
	if c.opts.NetworksWatcher != nil {
		c.opts.NetworksWatcher.AddNetworksHandler(c.reloadNetworkLookup)
		c.reloadMeshNetworks()
		c.reloadNetworkGateways()
	}
	c.informerInit.Store(true)

	kubelib.WaitForCacheSync(stop, c.informersSynced)
	// after informer caches sync the first time, process resources in order
	if err := c.SyncAll(); err != nil {
		log.Errorf("one or more errors force-syncing resources: %v", err)
	}
	c.initialSync.Store(true)
	log.Infof("kube controller for %s synced after %v", c.opts.ClusterID, time.Since(st))
	// after the in-order sync we can start processing the queue
	c.queue.Run(stop)
	log.Infof("Controller terminated")
}

// Stop the controller. Only for tests, to simplify the code (defer c.Stop())
func (c *Controller) Stop() {
	if c.stop != nil {
		close(c.stop)
	}
}

// Services implements a service catalog operation
func (c *Controller) Services() []*model.Service {
	c.RLock()
	out := make([]*model.Service, 0, len(c.servicesMap))
	for _, svc := range c.servicesMap {
		out = append(out, svc)
	}
	c.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

// GetService implements a service catalog operation by hostname specified.
func (c *Controller) GetService(hostname host.Name) *model.Service {
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()
	return svc
}

// getPodLocality retrieves the locality for a pod.
func (c *Controller) getPodLocality(pod *v1.Pod) string {
	// if pod has `istio-locality` label, skip below ops
	if len(pod.Labels[model.LocalityLabel]) > 0 {
		return model.GetLocalityLabelOrDefault(pod.Labels[model.LocalityLabel], "")
	}

	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#late-initialization
	raw, err := c.nodeLister.Get(pod.Spec.NodeName)
	if err != nil {
		if pod.Spec.NodeName != "" {
			log.Warnf("unable to get node %q for pod %q/%q: %v", pod.Spec.NodeName, pod.Namespace, pod.Name, err)
		}
		return ""
	}

	nodeMeta, err := meta.Accessor(raw)
	if err != nil {
		log.Warnf("unable to get node meta: %v", nodeMeta)
		return ""
	}

	region := getLabelValue(nodeMeta, NodeRegionLabel, NodeRegionLabelGA)
	zone := getLabelValue(nodeMeta, NodeZoneLabel, NodeZoneLabelGA)
	subzone := getLabelValue(nodeMeta, label.TopologySubzone.Name, "")

	if region == "" && zone == "" && subzone == "" {
		return ""
	}

	return region + "/" + zone + "/" + subzone // Format: "%s/%s/%s"
}

// InstancesByPort implements a service catalog operation
func (c *Controller) InstancesByPort(svc *model.Service, reqSvcPort int, labels labels.Instance) []*model.ServiceInstance {
	// First get k8s standard service instances and the workload entry instances
	outInstances := c.endpoints.InstancesByPort(c, svc, reqSvcPort, labels)
	outInstances = append(outInstances, c.serviceInstancesFromWorkloadInstances(svc, reqSvcPort)...)

	// return when instances found or an error occurs
	if len(outInstances) > 0 {
		return outInstances
	}

	// Fall back to external name service since we did not find any instances of normal services
	c.RLock()
	externalNameInstances := c.externalNameSvcInstanceMap[svc.Hostname]
	c.RUnlock()
	if externalNameInstances != nil {
		inScopeInstances := make([]*model.ServiceInstance, 0)
		for _, i := range externalNameInstances {
			if i.Service.Attributes.Namespace == svc.Attributes.Namespace && i.ServicePort.Port == reqSvcPort {
				inScopeInstances = append(inScopeInstances, i)
			}
		}
		return inScopeInstances
	}
	return nil
}

func (c *Controller) serviceInstancesFromWorkloadInstances(svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	// Run through all the workload instances, select ones that match the service labels
	// only if this is a kubernetes internal service and of ClientSideLB (eds) type
	// as InstancesByPort is called by the aggregate controller. We dont want to include
	// workload instances for any other registry
	workloadInstancesExist := !c.workloadInstancesIndex.Empty()
	c.RLock()
	_, inRegistry := c.servicesMap[svc.Hostname]
	c.RUnlock()

	// Only select internal Kubernetes services with selectors
	if !inRegistry || !workloadInstancesExist || svc.Attributes.ServiceRegistry != provider.Kubernetes ||
		svc.MeshExternal || svc.Resolution != model.ClientSideLB || svc.Attributes.LabelSelectors == nil {
		return nil
	}

	selector := labels.Instance(svc.Attributes.LabelSelectors)

	// Get the service port name and target port so that we can construct the service instance
	k8sService, err := c.serviceLister.Services(svc.Attributes.Namespace).Get(svc.Attributes.Name)
	// We did not find the k8s service. We cannot get the targetPort
	if err != nil {
		log.Infof("serviceInstancesFromWorkloadInstances(%s.%s) failed to get k8s service => error %v",
			svc.Attributes.Name, svc.Attributes.Namespace, err)
		return nil
	}

	var servicePort *model.Port
	for _, p := range svc.Ports {
		if p.Port == reqSvcPort {
			servicePort = p
			break
		}
	}
	if servicePort == nil {
		return nil
	}

	// Now get the target Port for this service port
	targetPort := findServiceTargetPort(servicePort, k8sService)
	if targetPort.num == 0 {
		targetPort.num = servicePort.Port
	}

	out := make([]*model.ServiceInstance, 0)

	c.workloadInstancesIndex.ForEach(func(wi *model.WorkloadInstance) {
		if wi.Namespace != svc.Attributes.Namespace {
			return
		}
		if selector.SubsetOf(wi.Endpoint.Labels) {
			instance := serviceInstanceFromWorkloadInstance(svc, servicePort, targetPort, wi)
			if instance != nil {
				out = append(out, instance)
			}
		}
	})
	return out
}

func serviceInstanceFromWorkloadInstance(svc *model.Service, servicePort *model.Port,
	targetPort serviceTargetPort, wi *model.WorkloadInstance) *model.ServiceInstance {
	// create an instance with endpoint whose service port name matches
	istioEndpoint := *wi.Endpoint

	// by default, use the numbered targetPort
	istioEndpoint.EndpointPort = uint32(targetPort.num)

	if targetPort.name != "" {
		// This is a named port, find the corresponding port in the port map
		matchedPort := wi.PortMap[targetPort.name]
		if matchedPort != 0 {
			istioEndpoint.EndpointPort = matchedPort
		} else if targetPort.explicitName {
			// No match found, and we expect the name explicitly in the service, skip this endpoint
			return nil
		}
	}

	istioEndpoint.ServicePortName = servicePort.Name
	return &model.ServiceInstance{
		Service:     svc,
		ServicePort: servicePort,
		Endpoint:    &istioEndpoint,
	}
}

// convenience function to collect all workload entry endpoints in updateEDS calls.
func (c *Controller) collectWorkloadInstanceEndpoints(svc *model.Service) []*model.IstioEndpoint {
	workloadInstancesExist := !c.workloadInstancesIndex.Empty()

	if !workloadInstancesExist || svc.Resolution != model.ClientSideLB || len(svc.Ports) == 0 {
		return nil
	}

	endpoints := make([]*model.IstioEndpoint, 0)
	for _, port := range svc.Ports {
		for _, instance := range c.serviceInstancesFromWorkloadInstances(svc, port.Port) {
			endpoints = append(endpoints, instance.Endpoint)
		}
	}

	return endpoints
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
// TODO: this code does not return k8s service instances when the proxy's IP is a workload entry
// To tackle this, we need a ip2instance map like what we have in service entry.
func (c *Controller) GetProxyServiceInstances(proxy *model.Proxy) []*model.ServiceInstance {
	if len(proxy.IPAddresses) > 0 {
		proxyIP := proxy.IPAddresses[0]
		// look up for a WorkloadEntry; if there are multiple WorkloadEntry(s)
		// with the same IP, choose one deterministically
		workload := workloadinstances.GetInstanceForProxy(c.workloadInstancesIndex, proxy, proxyIP)
		if workload != nil {
			return c.serviceInstancesFromWorkloadInstance(workload)
		}
		pod := c.pods.getPodByProxy(proxy)
		if pod != nil && !proxy.IsVM() {
			// we don't want to use this block for our test "VM" which is actually a Pod.

			if !c.isControllerForProxy(proxy) {
				log.Errorf("proxy is in cluster %v, but controller is for cluster %v", proxy.Metadata.ClusterID, c.Cluster())
				return nil
			}

			// 1. find proxy service by label selector, if not any, there may exist headless service without selector
			// failover to 2
			if services, err := getPodServices(c.serviceLister, pod); err == nil && len(services) > 0 {
				out := make([]*model.ServiceInstance, 0)
				for _, svc := range services {
					out = append(out, c.getProxyServiceInstancesByPod(pod, svc, proxy)...)
				}
				return out
			}
			// 2. Headless service without selector
			return c.endpoints.GetProxyServiceInstances(c, proxy)
		}

		// 3. The pod is not present when this is called
		// due to eventual consistency issues. However, we have a lot of information about the pod from the proxy
		// metadata already. Because of this, we can still get most of the information we need.
		// If we cannot accurately construct ServiceInstances from just the metadata, this will return an error and we can
		// attempt to read the real pod.
		out, err := c.getProxyServiceInstancesFromMetadata(proxy)
		if err != nil {
			log.Warnf("getProxyServiceInstancesFromMetadata for %v failed: %v", proxy.ID, err)
		}
		return out
	}

	// TODO: This could not happen, remove?
	if c.opts.Metrics != nil {
		c.opts.Metrics.AddMetric(model.ProxyStatusNoService, proxy.ID, proxy.ID, "")
	} else {
		log.Infof("Missing metrics env, empty list of services for pod %s", proxy.ID)
	}
	return nil
}

func (c *Controller) serviceInstancesFromWorkloadInstance(si *model.WorkloadInstance) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	// find the workload entry's service by label selector
	// rather than scanning through our internal map of model.services, get the services via the k8s apis
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: si.Namespace, Labels: si.Endpoint.Labels},
	}

	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	if k8sServices, err := getPodServices(c.serviceLister, dummyPod); err == nil && len(k8sServices) > 0 {
		for _, k8sSvc := range k8sServices {
			service := c.GetService(kube.ServiceHostname(k8sSvc.Name, k8sSvc.Namespace, c.opts.DomainSuffix))
			// Note that this cannot be an external service because k8s external services do not have label selectors.
			if service == nil || service.Resolution != model.ClientSideLB {
				// may be a headless service
				continue
			}

			for _, servicePort := range service.Ports {
				if servicePort.Protocol == protocol.UDP {
					continue
				}

				// Now get the target Port for this service port
				targetPort := findServiceTargetPort(servicePort, k8sSvc)
				if targetPort.num == 0 {
					targetPort.num = servicePort.Port
				}

				instance := serviceInstanceFromWorkloadInstance(service, servicePort, targetPort, si)
				if instance != nil {
					out = append(out, instance)
				}
			}
		}
	}
	return out
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (c *Controller) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	// ignore malformed workload entries. And ignore any workload entry that does not have a label
	// as there is no way for us to select them
	if si.Namespace == "" || len(si.Endpoint.Labels) == 0 {
		return
	}

	// this is from a workload entry. Store it in separate index so that
	// the InstancesByPort can use these as well as the k8s pods.
	switch event {
	case model.EventDelete:
		c.workloadInstancesIndex.Delete(si)
	default: // add or update
		c.workloadInstancesIndex.Insert(si)
	}

	// find the workload entry's service by label selector
	// rather than scanning through our internal map of model.services, get the services via the k8s apis
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: si.Namespace, Labels: si.Endpoint.Labels},
	}

	shard := model.ShardKeyFromRegistry(c)
	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	if k8sServices, err := getPodServices(c.serviceLister, dummyPod); err == nil && len(k8sServices) > 0 {
		for _, k8sSvc := range k8sServices {
			service := c.GetService(kube.ServiceHostname(k8sSvc.Name, k8sSvc.Namespace, c.opts.DomainSuffix))
			// Note that this cannot be an external service because k8s external services do not have label selectors.
			if service == nil || service.Resolution != model.ClientSideLB {
				// may be a headless service
				continue
			}

			// Get the updated list of endpoints that includes k8s pods and the workload entries for this service
			// and then notify the EDS server that endpoints for this service have changed.
			// We need one endpoint object for each service port
			endpoints := make([]*model.IstioEndpoint, 0)
			for _, port := range service.Ports {
				if port.Protocol == protocol.UDP {
					continue
				}
				// Similar code as UpdateServiceShards in eds.go
				instances := c.InstancesByPort(service, port.Port, nil)
				for _, inst := range instances {
					endpoints = append(endpoints, inst.Endpoint)
				}
			}
			// fire off eds update
			c.opts.XDSUpdater.EDSUpdate(shard, string(service.Hostname), service.Attributes.Namespace, endpoints)
		}
	}
}

func (c *Controller) onSystemNamespaceEvent(obj interface{}, ev model.Event) error {
	if ev == model.EventDelete {
		return nil
	}
	ns, ok := obj.(*v1.Namespace)
	if !ok {
		log.Warnf("Namespace watch getting wrong type in event: %T", obj)
		return nil
	}
	if ns == nil {
		return nil
	}
	nw := ns.Labels[label.TopologyNetwork.Name]
	c.Lock()
	oldDefaultNetwork := c.network
	c.network = network.ID(nw)
	c.Unlock()
	// network changed, rarely happen
	if oldDefaultNetwork != c.network {
		// refresh pods/endpoints/services
		c.onDefaultNetworkChange()
	}
	return nil
}

// isControllerForProxy should be used for proxies assumed to be in the kube cluster for this controller. Workload Entries
// may not necessarily pass this check, but we still want to allow kube services to select workload instances.
func (c *Controller) isControllerForProxy(proxy *model.Proxy) bool {
	return proxy.Metadata.ClusterID == "" || proxy.Metadata.ClusterID == c.Cluster()
}

// getProxyServiceInstancesFromMetadata retrieves ServiceInstances using proxy Metadata rather than
// from the Pod. This allows retrieving Instances immediately, regardless of delays in Kubernetes.
// If the proxy doesn't have enough metadata, an error is returned
func (c *Controller) getProxyServiceInstancesFromMetadata(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	if len(proxy.Metadata.Labels) == 0 {
		return nil, nil
	}

	if !c.isControllerForProxy(proxy) {
		return nil, fmt.Errorf("proxy is in cluster %v, but controller is for cluster %v", proxy.Metadata.ClusterID, c.Cluster())
	}

	// Create a pod with just the information needed to find the associated Services
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: proxy.ConfigNamespace,
			Labels:    proxy.Metadata.Labels,
		},
	}

	// Find the Service associated with the pod.
	services, err := getPodServices(c.serviceLister, dummyPod)
	if err != nil {
		return nil, fmt.Errorf("error getting instances for %s: %v", proxy.ID, err)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no instances found for %s: %v", proxy.ID, err)
	}

	out := make([]*model.ServiceInstance, 0)
	for _, svc := range services {
		hostname := kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)
		modelService := c.GetService(hostname)
		if modelService == nil {
			return nil, fmt.Errorf("failed to find model service for %v", hostname)
		}

		for _, modelService := range c.servicesForNamespacedName(kube.NamespacedNameForK8sObject(svc)) {
			discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(modelService)

			tps := make(map[model.Port]*model.Port)
			tpsList := make([]model.Port, 0)
			for _, port := range svc.Spec.Ports {
				svcPort, f := modelService.Ports.Get(port.Name)
				if !f {
					return nil, fmt.Errorf("failed to get svc port for %v", port.Name)
				}

				var portNum int
				if len(proxy.Metadata.PodPorts) > 0 {
					portNum, err = findPortFromMetadata(port, proxy.Metadata.PodPorts)
					if err != nil {
						return nil, fmt.Errorf("failed to find target port for %v: %v", proxy.ID, err)
					}
				} else {
					// most likely a VM - we assume the WorkloadEntry won't remap any ports
					portNum = port.TargetPort.IntValue()
				}

				// Dedupe the target ports here - Service might have configured multiple ports to the same target port,
				// we will have to create only one ingress listener per port and protocol so that we do not endup
				// complaining about listener conflicts.
				targetPort := model.Port{
					Port:     portNum,
					Protocol: svcPort.Protocol,
				}
				if _, exists := tps[targetPort]; !exists {
					tps[targetPort] = svcPort
					tpsList = append(tpsList, targetPort)
				}
			}

			epBuilder := NewEndpointBuilderFromMetadata(c, proxy)
			// Iterate over target ports in the same order as defined in service spec, in case of
			// protocol conflict for a port causes unstable protocol selection for a port.
			for _, tp := range tpsList {
				svcPort := tps[tp]
				// consider multiple IP scenarios
				for _, ip := range proxy.IPAddresses {
					// Construct the ServiceInstance
					out = append(out, &model.ServiceInstance{
						Service:     modelService,
						ServicePort: svcPort,
						Endpoint:    epBuilder.buildIstioEndpoint(ip, int32(tp.Port), svcPort.Name, discoverabilityPolicy),
					})
				}
			}
		}
	}
	return out, nil
}

func (c *Controller) getProxyServiceInstancesByPod(pod *v1.Pod,
	service *v1.Service, proxy *model.Proxy) []*model.ServiceInstance {
	var out []*model.ServiceInstance

	for _, svc := range c.servicesForNamespacedName(kube.NamespacedNameForK8sObject(service)) {
		discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(svc)

		tps := make(map[model.Port]*model.Port)
		tpsList := make([]model.Port, 0)
		for _, port := range service.Spec.Ports {
			svcPort, exists := svc.Ports.Get(port.Name)
			if !exists {
				continue
			}
			// find target port
			portNum, err := FindPort(pod, &port)
			if err != nil {
				log.Warnf("Failed to find port for service %s/%s: %v", service.Namespace, service.Name, err)
				continue
			}
			// Dedupe the target ports here - Service might have configured multiple ports to the same target port,
			// we will have to create only one ingress listener per port and protocol so that we do not endup
			// complaining about listener conflicts.
			targetPort := model.Port{
				Port:     portNum,
				Protocol: svcPort.Protocol,
			}
			if _, exists := tps[targetPort]; !exists {
				tps[targetPort] = svcPort
				tpsList = append(tpsList, targetPort)
			}
		}

		builder := NewEndpointBuilder(c, pod)
		// Iterate over target ports in the same order as defined in service spec, in case of
		// protocol conflict for a port causes unstable protocol selection for a port.
		for _, tp := range tpsList {
			svcPort := tps[tp]
			// consider multiple IP scenarios
			for _, ip := range proxy.IPAddresses {
				istioEndpoint := builder.buildIstioEndpoint(ip, int32(tp.Port), svcPort.Name, discoverabilityPolicy)
				out = append(out, &model.ServiceInstance{
					Service:     svc,
					ServicePort: svcPort,
					Endpoint:    istioEndpoint,
				})
			}
		}
	}

	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	pod := c.pods.getPodByProxy(proxy)
	if pod != nil {
		return pod.Labels
	}
	return nil
}

// GetIstioServiceAccounts returns the Istio service accounts running a service
// hostname. Each service account is encoded according to the SPIFFE VSID spec.
// For example, a service account named "bar" in namespace "foo" is encoded as
// "spiffe://cluster.local/ns/foo/sa/bar".
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	return model.GetServiceAccounts(svc, ports, c)
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) {
	c.handlers.AppendServiceHandler(f)
}

// AppendWorkloadHandler implements a service catalog operation
func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	c.handlers.AppendWorkloadHandler(f)
}
