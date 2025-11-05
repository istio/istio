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
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	pm "istio.io/istio/pkg/model"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/serviceregistry/util/workloadinstances"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
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
	// DefaultNetworkGatewayPort is the port used by default for cross-network traffic in ambient mode (when
	// double-HBONE protocol is used for communication).
	DefaultNetworkGatewayHBONEPort = 15008
)

var log = istiolog.RegisterScope("kube", "kubernetes service registry controller")

var (
	typeTag   = monitoring.CreateLabel("type")
	eventTag  = monitoring.CreateLabel("event")
	reasonTag = monitoring.CreateLabel("reason")

	k8sEvents = monitoring.NewSum(
		"pilot_k8s_reg_events",
		"Events from k8s registry.",
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

	proxyNoSvcTarget = monitoring.NewSum(
		"pilot_k8s_proxies_with_no_service_targets",
		"Number of proxies that do not have any corresponding service targets.",
	)
	proxyNoSvcTargetWrongCluster   = proxyNoSvcTarget.With(reasonTag.Value("incorrect_cluster"))
	proxyNoSvcTargetMissingService = proxyNoSvcTarget.With(reasonTag.Value("no_matching_services"))
	proxyNoSvcTargetFromMetadata   = proxyNoSvcTarget.With(reasonTag.Value("no_matching_metadata"))
)

// Options stores the configurable attributes of a Controller.
type Options struct {
	SystemNamespace string

	// MeshServiceController is a mesh-wide service Controller.
	MeshServiceController *aggregate.Controller

	DomainSuffix string

	// ClusterID identifies the cluster which the controller communicate with.
	ClusterID cluster.ID

	// ClusterAliases are alias names for cluster. When a proxy connects with a cluster ID
	// and if it has a different alias we should use that a cluster ID for proxy.
	ClusterAliases map[string]string

	// Metrics for capturing node-based metrics.
	Metrics model.Metrics

	// XDSUpdater will push changes to the xDS server.
	XDSUpdater model.XDSUpdater

	// MeshNetworksWatcher observes changes to the mesh networks config.
	MeshNetworksWatcher mesh.NetworksWatcher

	// MeshWatcher observes changes to the mesh config
	MeshWatcher meshwatcher.WatcherCollection

	// Maximum QPS when communicating with kubernetes API
	KubernetesAPIQPS float32

	// Maximum burst for throttle when communicating with the kubernetes API
	KubernetesAPIBurst int

	// SyncTimeout, if set, causes HasSynced to be returned when timeout.
	SyncTimeout time.Duration

	// Revision of this Istiod instance
	Revision string

	ConfigCluster bool

	CniNamespace string

	// StatusWritingEnabled determines if status writing is enabled. This may be set to `nil`, in which case status
	// writing will never be enabled
	StatusWritingEnabled *activenotifier.ActiveNotifier

	KrtDebugger *krt.DebugHandler
}

// kubernetesNode represents a kubernetes node that is reachable externally
type kubernetesNode struct {
	address string
	labels  labels.Instance
}

// controllerInterface is a simplified interface for the Controller used for testing.
type controllerInterface interface {
	Network(endpointIP string, labels labels.Instance) network.ID
}

var (
	_ controllerInterface      = &Controller{}
	_ serviceregistry.Instance = &Controller{}
)

type ambientIndex = ambient.Index

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	opts Options

	client kubelib.Client

	queue queue.Instance

	namespaces kclient.Client[*v1.Namespace]
	services   kclient.Client[*v1.Service]

	endpoints *endpointSliceController

	// Used to watch node accessible from remote cluster.
	// In multi-cluster(shared control plane multi-networks) scenario, ingress gateway service can be of nodePort type.
	// With this, we can populate mesh's gateway address with the node ips.
	nodes kclient.Client[*v1.Node]

	exports serviceExportCache
	imports serviceImportCache
	pods    *PodCache

	crdHandlers                []func(name string)
	handlers                   model.ControllerHandlers
	namespaceDiscoveryHandlers []func(ns string, event model.Event)

	// This is only used for test
	stop chan struct{}

	sync.RWMutex
	// servicesMap stores hostname ==> service, it is used to reduce convertService calls.
	servicesMap map[host.Name]*model.Service
	// nodeSelectorsForServices stores hostname => label selectors that can be used to
	// refine the set of node port IPs for a service.
	nodeSelectorsForServices map[host.Name]labels.Instance
	// map of node name and its address+labels - this is the only thing we need from nodes
	// for vm to k8s or cross cluster. When node port services select specific nodes by labels,
	// we run through the label selectors here to pick only ones that we need.
	// Only nodes with ExternalIP addresses are included in this map !
	nodeInfoMap map[string]kubernetesNode
	// index over workload instances from workload entries
	workloadInstancesIndex workloadinstances.Index

	*networkManager

	ambientIndex

	// initialSyncTimedout is set to true after performing an initial processing timed out.
	initialSyncTimedout *atomic.Bool
	meshWatcher         mesh.Watcher

	podsClient kclient.Client[*v1.Pod]

	configCluster bool

	networksHandlerRegistration *mesh.WatcherHandlerRegistration
}

// NewController creates a new Kubernetes controller
// Created by bootstrap and multicluster (see multicluster.Controller).
func NewController(kubeClient kubelib.Client, options Options) *Controller {
	c := &Controller{
		opts:                     options,
		client:                   kubeClient,
		queue:                    queue.NewQueueWithID(1*time.Second, string(options.ClusterID)),
		servicesMap:              make(map[host.Name]*model.Service),
		nodeSelectorsForServices: make(map[host.Name]labels.Instance),
		nodeInfoMap:              make(map[string]kubernetesNode),
		workloadInstancesIndex:   workloadinstances.NewIndex(),
		initialSyncTimedout:      atomic.NewBool(false),

		configCluster: options.ConfigCluster,
	}
	c.networkManager = initNetworkManager(c, options)

	// currently NOT using kubeClient.ObjectFilter() here as we only care about the system namespace
	// if in the future we want to watch other namespaces here, we will need to alter the discoveryNamespacesFilter
	// to have an exception for the system namespace
	c.namespaces = kclient.New[*v1.Namespace](kubeClient)

	if c.opts.SystemNamespace != "" {
		registerHandlers[*v1.Namespace](
			c,
			c.namespaces,
			"Namespaces",
			func(old *v1.Namespace, cur *v1.Namespace, event model.Event) error {
				if cur.Name == c.opts.SystemNamespace {
					return c.onSystemNamespaceEvent(old, cur, event)
				}
				return nil
			},
			nil,
		)
	}

	c.services = kclient.NewFiltered[*v1.Service](kubeClient, kclient.Filter{ObjectFilter: kubeClient.ObjectFilter()})

	registerHandlers(c, c.services, "Services", c.onServiceEvent, nil)

	c.endpoints = newEndpointSliceController(c)

	// This is for getting the node IPs of a selected set of nodes
	c.nodes = kclient.NewFiltered[*v1.Node](kubeClient, kclient.Filter{ObjectTransform: kubelib.StripNodeUnusedFields})
	registerHandlers[*v1.Node](c, c.nodes, "Nodes", c.onNodeEvent, nil)

	c.podsClient = kclient.NewFiltered[*v1.Pod](kubeClient, kclient.Filter{
		ObjectFilter:    kubeClient.ObjectFilter(),
		ObjectTransform: kubelib.StripPodUnusedFields,
	})
	c.pods = newPodCache(c, c.podsClient, func(key types.NamespacedName) {
		c.queue.Push(func() error {
			return c.endpoints.podArrived(key.Name, key.Namespace)
		})
	})
	registerHandlers[*v1.Pod](c, c.podsClient, "Pods", c.pods.onEvent, nil)

	if features.EnableAmbient && options.ConfigCluster {
		c.ambientIndex = ambient.New(ambient.Options{
			Client:          kubeClient,
			SystemNamespace: options.SystemNamespace,
			DomainSuffix:    options.DomainSuffix,
			ClusterID:       options.ClusterID,
			IsConfigCluster: options.ConfigCluster,
			Revision:        options.Revision,
			XDSUpdater:      options.XDSUpdater,
			MeshConfig:      options.MeshWatcher,
			StatusNotifier:  options.StatusWritingEnabled,
			Debugger:        options.KrtDebugger,
			Flags: ambient.FeatureFlags{
				DefaultAllowFromWaypoint:              features.DefaultAllowFromWaypoint,
				EnableK8SServiceSelectWorkloadEntries: features.EnableK8SServiceSelectWorkloadEntries,
			},
			ClientBuilder: multicluster.DefaultBuildClientsFromConfig,
			RemoteClientConfigOverrides: []func(*rest.Config){
				func(r *rest.Config) {
					r.QPS = options.KubernetesAPIQPS
					r.Burst = options.KubernetesAPIBurst
				},
			},
		})
	}

	c.exports = newServiceExportCache(c)
	c.imports = newServiceImportCache(c)

	c.meshWatcher = options.MeshWatcher
	if c.opts.MeshNetworksWatcher != nil {
		c.networksHandlerRegistration = c.opts.MeshNetworksWatcher.AddNetworksHandler(func() {
			c.reloadMeshNetworks()
			c.onNetworkChange()
		})
		c.reloadMeshNetworks()
	}
	return c
}

func (c *Controller) Provider() provider.ID {
	return provider.Kubernetes
}

func (c *Controller) Cluster() cluster.ID {
	return c.opts.ClusterID
}

func (c *Controller) MCSServices() []model.MCSServiceInfo {
	outMap := make(map[types.NamespacedName]model.MCSServiceInfo)

	// Add the ServiceExport info.
	for _, se := range c.exports.ExportedServices() {
		mcsService := outMap[se.namespacedName]
		mcsService.Cluster = c.Cluster()
		mcsService.Name = se.namespacedName.Name
		mcsService.Namespace = se.namespacedName.Namespace
		mcsService.Exported = true
		mcsService.Discoverability = se.discoverability
		outMap[se.namespacedName] = mcsService
	}

	// Add the ServiceImport info.
	for _, si := range c.imports.ImportedServices() {
		mcsService := outMap[si.namespacedName]
		mcsService.Cluster = c.Cluster()
		mcsService.Name = si.namespacedName.Name
		mcsService.Namespace = si.namespacedName.Namespace
		mcsService.Imported = true
		mcsService.ClusterSetVIP = si.clusterSetVIP
		outMap[si.namespacedName] = mcsService
	}

	return maps.Values(outMap)
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

	// Unregister networks handler
	if c.networksHandlerRegistration != nil {
		c.opts.MeshNetworksWatcher.DeleteNetworksHandler(c.networksHandlerRegistration)
	}

	// Shutdown all the informer handlers
	c.shutdownInformerHandlers()

	return nil
}

func (c *Controller) onServiceEvent(pre, curr *v1.Service, event model.Event) error {
	log.Debugf("Handle event %s for service %s in namespace %s", event, curr.Name, curr.Namespace)

	// Create the standard (cluster.local) service.
	svcConv := kube.ConvertService(*curr, c.opts.DomainSuffix, c.Cluster(), c.meshWatcher.Mesh())

	switch event {
	case model.EventDelete:
		c.deleteService(svcConv)
	default:
		c.addOrUpdateService(pre, curr, svcConv, event, false)
	}

	return nil
}

func (c *Controller) deleteService(svc *model.Service) {
	c.Lock()
	delete(c.servicesMap, svc.Hostname)
	delete(c.nodeSelectorsForServices, svc.Hostname)
	c.Unlock()

	c.networkManager.Lock()
	_, isNetworkGateway := c.networkGatewaysBySvc[svc.Hostname]
	delete(c.networkGatewaysBySvc, svc.Hostname)
	c.networkManager.Unlock()
	if isNetworkGateway {
		c.NotifyGatewayHandlers()
		// TODO trigger push via handler
		// networks are different, we need to update all eds endpoints
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: model.NewReasonStats(model.NetworksTrigger), Forced: true})
	}

	shard := model.ShardKeyFromRegistry(c)
	event := model.EventDelete
	c.opts.XDSUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, event)
	if !svc.Attributes.ExportTo.Contains(visibility.None) {
		c.handlers.NotifyServiceHandlers(nil, svc, event)
	}
}

// recomputeServiceForPod is called when a pod changes and service endpoints need to be recomputed.
// Most of Pod is immutable, so once it has been created we are ok to cache the internal representation.
// However, a few fields (labels) are mutable. When these change, we call recomputeServiceForPod and rebuild the cache
// for all service's the pod is a part of and push an update.
func (c *Controller) recomputeServiceForPod(pod *v1.Pod) {
	allServices := c.services.List(pod.Namespace, klabels.Everything())
	cu := sets.New[model.ConfigKey]()
	services := getPodServices(allServices, pod)
	for _, svc := range services {
		hostname := kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)
		c.Lock()
		conv, f := c.servicesMap[hostname]
		c.Unlock()
		if !f {
			return
		}
		shard := model.ShardKeyFromRegistry(c)
		endpoints := c.buildEndpointsForService(conv, true)
		if len(endpoints) > 0 {
			c.opts.XDSUpdater.EDSCacheUpdate(shard, string(hostname), svc.Namespace, endpoints)
		}
		cu.Insert(model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(hostname),
			Namespace: svc.Namespace,
		})
	}
	if len(cu) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.EndpointUpdate),
		})
	}
}

func (c *Controller) addOrUpdateService(pre, curr *v1.Service, currConv *model.Service, event model.Event, updateEDSCache bool) {
	needsFullPush := false
	// First, process nodePort gateway service, whose externalIPs specified
	// and loadbalancer gateway service
	if currConv.Attributes.ClusterExternalAddresses.Len() > 0 {
		needsFullPush = c.extractGatewaysFromService(currConv)
	} else if isNodePortGatewayService(curr) {
		// We need to know which services are using node selectors because during node events,
		// we have to update all the node port services accordingly.
		nodeSelector := getNodeSelectorsForService(curr)
		c.Lock()
		// only add when it is nodePort gateway service
		c.nodeSelectorsForServices[currConv.Hostname] = nodeSelector
		c.Unlock()
		needsFullPush = c.updateServiceNodePortAddresses(currConv)
	}

	c.Lock()
	prevConv := c.servicesMap[currConv.Hostname]
	c.servicesMap[currConv.Hostname] = currConv
	c.Unlock()
	// This full push needed to update all endpoints, even though we do a full push on service add/update
	// as that full push is only triggered for the specific service.
	if needsFullPush {
		// networks are different, we need to update all eds endpoints
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: model.NewReasonStats(model.NetworksTrigger), Forced: true})
	}

	shard := model.ShardKeyFromRegistry(c)
	ns := currConv.Attributes.Namespace
	// We also need to update when the Service changes. For Kubernetes, a service change will result in Endpoint updates,
	// but workload entries will also need to be updated.
	// TODO(nmittler): Build different sets of endpoints for cluster.local and clusterset.local.
	if updateEDSCache || features.EnableK8SServiceSelectWorkloadEntries {
		endpoints := c.buildEndpointsForService(currConv, updateEDSCache)
		if len(endpoints) > 0 {
			c.opts.XDSUpdater.EDSCacheUpdate(shard, string(currConv.Hostname), ns, endpoints)
		}
	}

	c.opts.XDSUpdater.SvcUpdate(shard, string(currConv.Hostname), ns, event)
	if serviceUpdateNeedsPush(pre, curr, prevConv, currConv) {
		log.Debugf("Service %s in namespace %s updated and needs push", currConv.Hostname, ns)
		c.handlers.NotifyServiceHandlers(prevConv, currConv, event)
	}
}

func (c *Controller) buildEndpointsForService(svc *model.Service, updateCache bool) []*model.IstioEndpoint {
	endpoints := c.endpoints.buildIstioEndpointsWithService(svc.Attributes.Name, svc.Attributes.Namespace, svc.Hostname, updateCache)
	if features.EnableK8SServiceSelectWorkloadEntries {
		fep := c.collectWorkloadInstanceEndpoints(svc)
		endpoints = append(endpoints, fep...)
	}
	return endpoints
}

func (c *Controller) onNodeEvent(_, node *v1.Node, event model.Event) error {
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
			Reason: model.NewReasonStats(model.ServiceUpdate),
			Forced: true,
		})
	}
	return nil
}

// FilterOutFunc func for filtering out objects during update callback
type FilterOutFunc[T controllers.Object] func(old, cur T) bool

// registerHandlers registers a handler for a given informer
// Note: `otype` is used for metric, if empty, no metric will be reported
func registerHandlers[T controllers.ComparableObject](c *Controller,
	informer kclient.Informer[T], otype string,
	handler func(T, T, model.Event) error, filter FilterOutFunc[T],
) {
	wrappedHandler := func(prev, curr T, event model.Event) error {
		curr = informer.Get(curr.GetName(), curr.GetNamespace())
		if controllers.IsNil(curr) {
			// this can happen when an immediate delete after update
			// the delete event can be handled later
			return nil
		}
		return handler(prev, curr, event)
	}
	// Pre-build our metric types to avoid recompute them on each event
	adds := k8sEvents.With(typeTag.Value(otype), eventTag.Value("add"))
	updatesames := k8sEvents.With(typeTag.Value(otype), eventTag.Value("updatesame"))
	updates := k8sEvents.With(typeTag.Value(otype), eventTag.Value("update"))
	deletes := k8sEvents.With(typeTag.Value(otype), eventTag.Value("delete"))

	informer.AddEventHandler(
		controllers.EventHandler[T]{
			AddFunc: func(obj T) {
				adds.Increment()
				c.queue.Push(func() error {
					return wrappedHandler(ptr.Empty[T](), obj, model.EventAdd)
				})
			},
			UpdateFunc: func(old, cur T) {
				if filter != nil {
					if filter(old, cur) {
						updatesames.Increment()
						return
					}
				}
				updates.Increment()
				c.queue.Push(func() error {
					return wrappedHandler(old, cur, model.EventUpdate)
				})
			},
			DeleteFunc: func(obj T) {
				deletes.Increment()
				c.queue.Push(func() error {
					return handler(ptr.Empty[T](), obj, model.EventDelete)
				})
			},
		})
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	if c.initialSyncTimedout.Load() {
		return true
	}
	if c.ambientIndex != nil && !c.ambientIndex.HasSynced() {
		return false
	}
	return c.queue.HasSynced()
}

func (c *Controller) shutdownInformerHandlers() {
	c.namespaces.ShutdownHandlers()
	c.services.ShutdownHandlers()
	c.endpoints.slices.ShutdownHandlers()
	c.pods.pods.ShutdownHandlers()
	c.nodes.ShutdownHandlers()
}

func (c *Controller) informersSynced() bool {
	return c.namespaces.HasSynced() &&
		c.services.HasSynced() &&
		c.endpoints.slices.HasSynced() &&
		c.pods.pods.HasSynced() &&
		c.nodes.HasSynced() &&
		c.imports.HasSynced() &&
		c.exports.HasSynced() &&
		c.networkManager.HasSynced()
}

func (c *Controller) syncPods() error {
	var err *multierror.Error
	pods := c.podsClient.List(metav1.NamespaceAll, klabels.Everything())
	log.Debugf("initializing %d pods", len(pods))
	for _, s := range pods {
		err = multierror.Append(err, c.pods.onEvent(nil, s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	if c.opts.SyncTimeout != 0 {
		time.AfterFunc(c.opts.SyncTimeout, func() {
			if !c.queue.HasSynced() {
				log.Warnf("kube controller for %s initial sync timed out", c.opts.ClusterID)
				c.initialSyncTimedout.Store(true)
			}
		})
	}
	st := time.Now()

	go c.imports.Run(stop)
	go c.exports.Run(stop)
	if c.ambientIndex != nil {
		go c.ambientIndex.Run(stop)
	}
	kubelib.WaitForCacheSync("kube controller", stop, c.informersSynced)
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
	localityLabel := pm.GetLocalityLabel(pod.Labels)
	if localityLabel != "" {
		return pm.SanitizeLocalityLabel(localityLabel)
	}

	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#late-initialization
	node := c.nodes.Get(pod.Spec.NodeName, "")
	if node == nil {
		if pod.Spec.NodeName != "" {
			log.Warnf("unable to get node %q for pod %q/%q", pod.Spec.NodeName, pod.Namespace, pod.Name)
		}
		return ""
	}

	region := getLabelValue(node.ObjectMeta, NodeRegionLabelGA, NodeRegionLabel)
	zone := getLabelValue(node.ObjectMeta, NodeZoneLabelGA, NodeZoneLabel)
	subzone := getLabelValue(node.ObjectMeta, label.TopologySubzone.Name, "")

	if region == "" && zone == "" && subzone == "" {
		return ""
	}

	return region + "/" + zone + "/" + subzone // Format: "%s/%s/%s"
}

func (c *Controller) serviceInstancesFromWorkloadInstances(svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	// Run through all the workload instances, select ones that match the service labels
	// only if this is a kubernetes internal service and of ClientSideLB (eds) type
	// as InstancesByPort is called by the aggregate controller. We don't want to include
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
	k8sService := c.services.Get(svc.Attributes.Name, svc.Attributes.Namespace)
	// We did not find the k8s service. We cannot get the targetPort
	if k8sService == nil {
		log.Infof("serviceInstancesFromWorkloadInstances(%s.%s) failed to get k8s service",
			svc.Attributes.Name, svc.Attributes.Namespace)
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
		if selector.Match(wi.Endpoint.Labels) {
			instance := serviceInstanceFromWorkloadInstance(svc, servicePort, targetPort, wi)
			if instance != nil {
				out = append(out, instance)
			}
		}
	})
	return out
}

func serviceInstanceFromWorkloadInstance(svc *model.Service, servicePort *model.Port,
	targetPort serviceTargetPort, wi *model.WorkloadInstance,
) *model.ServiceInstance {
	// create an instance with endpoint whose service port name matches
	istioEndpoint := wi.Endpoint.ShallowCopy()

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
		Endpoint:    istioEndpoint,
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

// GetProxyServiceTargets returns service targets co-located with a given proxy
func (c *Controller) GetProxyServiceTargets(proxy *model.Proxy) []model.ServiceTarget {
	if !c.isControllerForProxy(proxy) {
		log.Errorf("proxy is in cluster %v, but controller is for cluster %v", proxy.Metadata.ClusterID, c.Cluster())
		proxyNoSvcTargetWrongCluster.Increment()
		return nil
	}

	if len(proxy.IPAddresses) > 0 {
		proxyIP := proxy.IPAddresses[0]
		// look up for a WorkloadEntry; if there are multiple WorkloadEntry(s)
		// with the same IP, choose one deterministically
		workload := workloadinstances.GetInstanceForProxy(c.workloadInstancesIndex, proxy, proxyIP)
		if workload != nil {
			return c.serviceTargetsFromWorkloadInstance(workload)
		}
		pod := c.pods.getPodByProxy(proxy)
		if pod != nil && !proxy.IsVM() {
			// we don't want to use this block for our test "VM" which is actually a Pod.

			// 1. find proxy service by label selector, if not any, there may exist headless service without selector
			// failover to 2
			allServices := c.services.List(pod.Namespace, klabels.Everything())
			if services := getPodServices(allServices, pod); len(services) > 0 {
				out := make([]model.ServiceTarget, 0)
				for _, svc := range services {
					out = append(out, c.GetProxyServiceTargetsByPod(pod, svc)...)
				}
				return out
			}
			// 2. Headless service without selector
			out := c.endpoints.GetProxyServiceTargets(proxy)
			if len(out) == 0 {
				proxyNoSvcTargetMissingService.Increment()
			}
			return out
		}

		// 3. The pod is not present when this is called
		// due to eventual consistency issues. However, we have a lot of information about the pod from the proxy
		// metadata already. Because of this, we can still get most of the information we need.
		// If we cannot accurately construct ServiceEndpoints from just the metadata, this will return an error and we can
		// attempt to read the real pod.
		out, err := c.GetProxyServiceTargetsFromMetadata(proxy)
		if err != nil {
			log.Errorf("failed to get proxy service targets from metadata: %v", err)
		}
		if len(out) == 0 {
			proxyNoSvcTargetFromMetadata.Increment()
		}
		return out
	}

	return nil
}

func (c *Controller) serviceTargetsFromWorkloadInstance(si *model.WorkloadInstance) []model.ServiceTarget {
	out := make([]model.ServiceTarget, 0)
	// find the workload entry's service by label selector
	// rather than scanning through our internal map of model.services, get the services via the k8s apis
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: si.Namespace, Labels: si.Endpoint.Labels},
	}

	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	allServices := c.services.List(si.Namespace, klabels.Everything())
	if k8sServices := getPodServices(allServices, dummyPod); len(k8sServices) > 0 {
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
					out = append(out, model.ServiceInstanceToTarget(instance))
				}
			}
		}
	}
	return out
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (c *Controller) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	c.queue.Push(func() error {
		c.workloadInstanceHandler(si, event)
		return nil
	})
}

func (c *Controller) workloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
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

	// We got an instance update, which probably effects EDS. However, EDS is keyed by Hostname. We need to find all
	// Hostnames (services) that were updated and recompute them
	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	allServices := c.services.List(si.Namespace, klabels.Everything())
	matchedServices := getPodServices(allServices, dummyPod)
	matchedHostnames := slices.Map(matchedServices, func(e *v1.Service) host.Name {
		return kube.ServiceHostname(e.Name, e.Namespace, c.opts.DomainSuffix)
	})
	c.endpoints.pushEDS(matchedHostnames, si.Namespace)
}

func (c *Controller) onSystemNamespaceEvent(_, ns *v1.Namespace, ev model.Event) error {
	if ev == model.EventDelete {
		return nil
	}
	if c.setNetworkFromNamespace(ns) {
		// network changed, rarely happen
		// refresh pods/endpoints/services
		c.onNetworkChange()
	}
	return nil
}

// isControllerForProxy should be used for proxies assumed to be in the kube cluster for this controller. Workload Entries
// may not necessarily pass this check, but we still want to allow kube services to select workload instances.
func (c *Controller) isControllerForProxy(proxy *model.Proxy) bool {
	return proxy.Metadata.ClusterID == "" || proxy.Metadata.ClusterID == c.Cluster()
}

// GetProxyServiceTargetsFromMetadata retrieves ServiceTargets using proxy Metadata rather than
// from the Pod. This allows retrieving Instances immediately, regardless of delays in Kubernetes.
// If the proxy doesn't have enough metadata, an error is returned
func (c *Controller) GetProxyServiceTargetsFromMetadata(proxy *model.Proxy) ([]model.ServiceTarget, error) {
	if len(proxy.Labels) == 0 {
		return nil, nil
	}

	// Create a pod with just the information needed to find the associated Services
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: proxy.ConfigNamespace,
			Labels:    proxy.Labels,
		},
	}

	// Find the Service associated with the pod.
	allServices := c.services.List(proxy.ConfigNamespace, klabels.Everything())
	services := getPodServices(allServices, dummyPod)
	if len(services) == 0 {
		return nil, fmt.Errorf("no instances found for %s", proxy.ID)
	}

	out := make([]model.ServiceTarget, 0)
	for _, svc := range services {
		hostname := kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)
		modelService := c.GetService(hostname)
		if modelService == nil {
			return nil, fmt.Errorf("failed to find model service for %v", hostname)
		}

		for _, modelService := range c.servicesForNamespacedName(config.NamespacedName(svc)) {
			tps := make(map[model.Port]*model.Port)
			tpsList := make([]model.Port, 0)
			for _, port := range svc.Spec.Ports {
				svcPort, f := modelService.Ports.Get(port.Name)
				if !f {
					return nil, fmt.Errorf("failed to get svc port for %v", port.Name)
				}

				var portNum int
				if len(proxy.Metadata.PodPorts) > 0 {
					var err error
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

			// Iterate over target ports in the same order as defined in service spec, in case of
			// protocol conflict for a port causes unstable protocol selection for a port.
			for _, tp := range tpsList {
				svcPort := tps[tp]
				out = append(out, model.ServiceTarget{
					Service: modelService,
					Port: model.ServiceInstancePort{
						ServicePort: svcPort,
						TargetPort:  uint32(tp.Port),
					},
				})
			}
		}
	}
	return out, nil
}

func (c *Controller) GetProxyServiceTargetsByPod(pod *v1.Pod, service *v1.Service) []model.ServiceTarget {
	var out []model.ServiceTarget

	for _, svc := range c.servicesForNamespacedName(config.NamespacedName(service)) {
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
				log.Debugf("Failed to find port for service %s/%s: %v", service.Namespace, service.Name, err)
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
		// Iterate over target ports in the same order as defined in service spec, in case of
		// protocol conflict for a port causes unstable protocol selection for a port.
		for _, tp := range tpsList {
			svcPort := tps[tp]
			out = append(out, model.ServiceTarget{
				Service: svc,
				Port: model.ServiceInstancePort{
					ServicePort: svcPort,
					TargetPort:  uint32(tp.Port),
				},
			})
		}
	}

	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	pod := c.pods.getPodByProxy(proxy)
	if pod != nil {
		locality := c.getPodLocality(pod)
		nodeName := proxy.GetNodeName()
		return labelutil.AugmentLabels(pod.Labels, c.clusterID, locality, nodeName, c.Network(pod.Status.PodIP, pod.Labels))
	}
	return nil
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f model.ServiceHandler) {
	c.handlers.AppendServiceHandler(f)
}

// AppendWorkloadHandler implements a service catalog operation
func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) {
	c.handlers.AppendWorkloadHandler(f)
}

// AppendNamespaceDiscoveryHandlers register handlers on namespace selected/deselected by discovery selectors change.
func (c *Controller) AppendNamespaceDiscoveryHandlers(f func(string, model.Event)) {
	c.namespaceDiscoveryHandlers = append(c.namespaceDiscoveryHandlers, f)
}

// AppendCrdHandlers register handlers on crd event.
func (c *Controller) AppendCrdHandlers(f func(name string)) {
	c.crdHandlers = append(c.crdHandlers, f)
}

// hostNamesForNamespacedName returns all possible hostnames for the given service name.
// If Kubernetes Multi-Cluster Services (MCS) is enabled, this will contain the regular
// hostname as well as the MCS hostname (clusterset.local). Otherwise, only the regular
// hostname will be returned.
func (c *Controller) hostNamesForNamespacedName(name types.NamespacedName) []host.Name {
	if features.EnableMCSHost {
		return []host.Name{
			kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix),
			serviceClusterSetLocalHostname(name),
		}
	}
	return []host.Name{
		kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix),
	}
}

// servicesForNamespacedName returns all services for the given service name.
// If Kubernetes Multi-Cluster Services (MCS) is enabled, this will contain the regular
// service as well as the MCS service (clusterset.local), if available. Otherwise,
// only the regular service will be returned.
func (c *Controller) servicesForNamespacedName(name types.NamespacedName) []*model.Service {
	if features.EnableMCSHost {
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
	if svc := c.GetService(kube.ServiceHostname(name.Name, name.Namespace, c.opts.DomainSuffix)); svc != nil {
		return []*model.Service{svc}
	}
	return nil
}

func serviceUpdateNeedsPush(prev, curr *v1.Service, preConv, currConv *model.Service) bool {
	// New Service - If it is not exported, no need to push.
	if preConv == nil {
		return !currConv.Attributes.ExportTo.Contains(visibility.None)
	}
	// if service Visibility is None and has not changed in the update/delete, no need to push.
	if preConv.Attributes.ExportTo.Contains(visibility.None) &&
		currConv.Attributes.ExportTo.Contains(visibility.None) {
		return false
	}
	// Check if there are any changes we care about by comparing `model.Service`s
	if !preConv.Equals(currConv) {
		return true
	}
	// Also check if target ports are changed since they are not included in `model.Service`
	// `preConv.Equals(currConv)` already makes sure the length of ports is not changed
	if prev != nil && curr != nil {
		if !slices.EqualFunc(prev.Spec.Ports, curr.Spec.Ports, func(a, b v1.ServicePort) bool {
			return a.TargetPort == b.TargetPort
		}) {
			return true
		}
	}
	return false
}
