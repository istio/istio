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
	"github.com/yl2chen/cidranger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

const (
	// NodeRegionLabel is the well-known label for kubernetes node region in beta
	NodeRegionLabel = "failure-domain.beta.kubernetes.io/region"
	// NodeZoneLabel is the well-known label for kubernetes node zone in beta
	NodeZoneLabel = "failure-domain.beta.kubernetes.io/zone"
	// NodeRegionLabelGA is the well-known label for kubernetes node region in ga
	NodeRegionLabelGA = "topology.kubernetes.io/region"
	// NodeZoneLabelGA is the well-known label for kubernetes node zone in ga
	NodeZoneLabelGA = "topology.kubernetes.io/zone"
	// IstioSubzoneLabel is custom subzone label for locality-based routing in Kubernetes see: https://github.com/istio/istio/issues/19114
	IstioSubzoneLabel = "topology.istio.io/subzone"
	// IstioNamespace used by default for Istio cluster-wide installation
	IstioNamespace = "istio-system"
)

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
	// Namespace the controller watches. If set to meta_v1.NamespaceAll (""), controller watches all namespaces
	WatchedNamespaces string
	ResyncPeriod      time.Duration
	DomainSuffix      string

	// ClusterID identifies the remote cluster in a multicluster env.
	ClusterID string

	// FetchCaRoot defines the function to get caRoot
	FetchCaRoot func() map[string]string

	// Metrics for capturing node-based metrics.
	Metrics model.Metrics

	// XDSUpdater will push changes to the xDS server.
	XDSUpdater model.XDSUpdater

	// TrustDomain used in SPIFFE identity
	TrustDomain string

	// NetworksWatcher observes changes to the mesh networks config.
	NetworksWatcher mesh.NetworksWatcher

	// EndpointMode decides what source to use to get endpoint information
	EndpointMode EndpointMode

	// CABundlePath defines the caBundle path for istiod Server
	CABundlePath string
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

var EndpointModeNames = map[EndpointMode]string{
	EndpointsOnly:     "EndpointsOnly",
	EndpointSliceOnly: "EndpointSliceOnly",
}

func (m EndpointMode) String() string {
	return EndpointModeNames[m]
}

var _ serviceregistry.Instance = &Controller{}

// kubernetesNode represents a kubernetes node that is reachable externally
type kubernetesNode struct {
	address string
	labels  labels.Instance
}

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	client kubernetes.Interface

	queue queue.Instance

	serviceInformer cache.SharedIndexInformer
	serviceLister   listerv1.ServiceLister

	endpoints kubeEndpointsController

	// Used to watch node accessible from remote cluster.
	// In multi-cluster(shared control plane multi-networks) scenario, ingress gateway service can be of nodePort type.
	// With this, we can populate mesh's gateway address with the node ips.
	nodeInformer cache.SharedIndexInformer
	nodeLister   listerv1.NodeLister

	pods *PodCache

	metrics         model.Metrics
	networksWatcher mesh.NetworksWatcher
	xdsUpdater      model.XDSUpdater
	domainSuffix    string
	clusterID       string

	serviceHandlers  []func(*model.Service, model.Event)
	workloadHandlers []func(*model.WorkloadInstance, model.Event)

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
	// externalNameSvcInstanceMap stores hostname ==> instance, is used to store instances for ExternalName k8s services
	externalNameSvcInstanceMap map[host.Name][]*model.ServiceInstance
	// workload instances from workload entries  - map of ip -> workload instance
	workloadInstancesByIP map[string]*model.WorkloadInstance
	// Stores a map of workload instance name/namespace to address
	workloadInstancesIPsByName map[string]string

	// CIDR ranger based on path-compressed prefix trie
	ranger cidranger.Ranger

	// Network name for the registry as specified by the MeshNetworks configmap
	networkForRegistry string

	// initialSync becomes true only after the resources that exist at startup are processed
	// to ensure correct ordering
	syncMu      sync.Mutex
	initialSync bool
}

// NewController creates a new Kubernetes controller
// Created by bootstrap and multicluster (see secretcontroler).
func NewController(kubeClient kubelib.Client, options Options) *Controller {
	log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)

	// The queue requires a time duration for a retry delay after a handler error
	c := &Controller{
		domainSuffix:               options.DomainSuffix,
		client:                     kubeClient.Kube(),
		queue:                      queue.NewQueue(1 * time.Second),
		clusterID:                  options.ClusterID,
		xdsUpdater:                 options.XDSUpdater,
		servicesMap:                make(map[host.Name]*model.Service),
		nodeSelectorsForServices:   make(map[host.Name]labels.Instance),
		nodeInfoMap:                make(map[string]kubernetesNode),
		externalNameSvcInstanceMap: make(map[host.Name][]*model.ServiceInstance),
		workloadInstancesByIP:      make(map[string]*model.WorkloadInstance),
		workloadInstancesIPsByName: make(map[string]string),
		networksWatcher:            options.NetworksWatcher,
		metrics:                    options.Metrics,
	}

	c.serviceInformer = kubeClient.KubeInformer().Core().V1().Services().Informer()
	c.serviceLister = kubeClient.KubeInformer().Core().V1().Services().Lister()
	registerHandlers(c.serviceInformer, c.queue, "Services", c.onServiceEvent, nil)

	switch options.EndpointMode {
	case EndpointsOnly:
		c.endpoints = newEndpointsController(c, kubeClient.KubeInformer().Core().V1().Endpoints())
	case EndpointSliceOnly:
		c.endpoints = newEndpointSliceController(c, kubeClient.KubeInformer().Discovery().V1alpha1().EndpointSlices())
	}

	// This is for getting the node IPs of a selected set of nodes
	c.nodeInformer = kubeClient.KubeInformer().Core().V1().Nodes().Informer()
	c.nodeLister = kubeClient.KubeInformer().Core().V1().Nodes().Lister()
	registerHandlers(c.nodeInformer, c.queue, "Nodes", c.onNodeEvent, nil)

	c.pods = newPodCache(c, kubeClient.KubeInformer().Core().V1().Pods(), func(key string) {
		item, exists, err := c.endpoints.getInformer().GetStore().GetByKey(key)
		if err != nil {
			log.Debugf("Endpoint %v lookup failed with error %v, skipping stale endpoint", key, err)
			return
		}
		if !exists {
			log.Debugf("Endpoint %v not found, skipping stale endpoint", key)
			return
		}
		c.queue.Push(func() error {
			return c.endpoints.onEvent(item, model.EventUpdate)
		})
	})
	registerHandlers(c.pods.informer, c.queue, "Pods", c.pods.onEvent, nil)

	return c
}

func (c *Controller) Provider() serviceregistry.ProviderID {
	return serviceregistry.Kubernetes
}

func (c *Controller) Cluster() string {
	return c.clusterID
}

func (c *Controller) Cleanup() error {
	// TODO(landow) do we need to cleanup other things besides endpoint shards?
	svcs, err := c.serviceLister.List(klabels.NewSelector())
	if err != nil {
		return fmt.Errorf("error listing services for deletion: %v", err)
	}
	for _, s := range svcs {
		name := kube.ServiceHostname(s.Namespace, s.Namespace, c.domainSuffix)
		c.xdsUpdater.SvcUpdate(c.clusterID, string(name), s.Namespace, model.EventDelete)
		// TODO(landow) do we need to notify service handlers?
	}
	return nil
}

func (c *Controller) onServiceEvent(curr interface{}, event model.Event) error {
	svc, ok := curr.(*v1.Service)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", curr)
			return nil
		}
		svc, ok = tombstone.Obj.(*v1.Service)
		if !ok {
			log.Errorf("Tombstone contained object that is not a service %#v", curr)
			return nil
		}
	}

	log.Debugf("Handle event %s for service %s in namespace %s", event, svc.Name, svc.Namespace)

	svcConv := kube.ConvertService(*svc, c.domainSuffix, c.clusterID)
	switch event {
	case model.EventDelete:
		c.Lock()
		delete(c.servicesMap, svcConv.Hostname)
		delete(c.nodeSelectorsForServices, svcConv.Hostname)
		delete(c.externalNameSvcInstanceMap, svcConv.Hostname)
		c.Unlock()
	default:
		if isNodePortGatewayService(svc) {
			// We need to know which services are using node selectors because during node events,
			// we have to update all the node port services accordingly.
			nodeSelector := getNodeSelectorsForService(svc)
			c.Lock()
			// only add when it is nodePort gateway service
			c.nodeSelectorsForServices[svcConv.Hostname] = nodeSelector
			c.Unlock()
			c.updateServiceExternalAddr(svcConv)
		}
		// instance conversion is only required when service is added/updated.
		instances := kube.ExternalNameServiceInstances(svc, svcConv)
		c.Lock()
		c.servicesMap[svcConv.Hostname] = svcConv
		if len(instances) > 0 {
			c.externalNameSvcInstanceMap[svcConv.Hostname] = instances
		}
		c.Unlock()
	}

	// We also need to update when the Service changes. For Kubernetes, a service change will result in Endpoint updates,
	// but workload entries will also need to be updated.
	if event == model.EventAdd || event == model.EventUpdate {
		// Build IstioEndpoints
		endpoints := c.endpoints.buildIstioEndpointsWithService(svc.Name, svc.Namespace, svcConv.Hostname)
		var fep []*model.IstioEndpoint
		if features.EnableK8SServiceSelectWorkloadEntries {
			fep = c.collectWorkloadInstanceEndpoints(svcConv)
			endpoints = append(endpoints, fep...)
		}

		// Only trigger update if workload entry changed. Otherwise, Endpoint event will trigger it once it comes in.
		if len(endpoints) > 0 && len(fep) > 0 {
			_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(svcConv.Hostname), svc.Namespace, endpoints)
		}
	}

	c.xdsUpdater.SvcUpdate(c.clusterID, string(svcConv.Hostname), svc.Namespace, event)
	// Notify service handlers.
	for _, f := range c.serviceHandlers {
		f(svcConv, event)
	}

	return nil
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
	if updatedNeeded && c.updateServiceExternalAddr() {
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full: true,
		})
	}
	return nil
}

// Filter func for filtering out objects during update callback
type FilterOutFunc func(old, cur interface{}) bool

func registerHandlers(informer cache.SharedIndexInformer, q queue.Instance, otype string,
	handler func(interface{}, model.Event) error, filter FilterOutFunc) {
	if filter == nil {
		filter = func(old, cur interface{}) bool {
			oldObj := old.(metav1.Object)
			newObj := cur.(metav1.Object)
			// TODO: this is only for test, add resource version for test
			if oldObj.GetResourceVersion() == "" || newObj.GetResourceVersion() == "" {
				return false
			}
			return oldObj.GetResourceVersion() == newObj.GetResourceVersion()
		}
	}

	wrappedHandler := func(obj interface{}, event model.Event) error {
		obj = tryGetLatestObject(informer, obj)
		return handler(obj, event)
	}

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				incrementEvent(otype, "add")
				q.Push(func() error {
					return wrappedHandler(obj, model.EventAdd)
				})
			},
			UpdateFunc: func(old, cur interface{}) {
				if !filter(old, cur) {
					incrementEvent(otype, "update")
					q.Push(func() error {
						return wrappedHandler(cur, model.EventUpdate)
					})
				} else {
					incrementEvent(otype, "updatesame")
				}
			},
			DeleteFunc: func(obj interface{}) {
				incrementEvent(otype, "delete")
				q.Push(func() error {
					return handler(obj, model.EventDelete)
				})
			},
		})
}

// tryGetLatestObject attempts to fetch the latest version of the object from the cache.
// Changes may have occurred between queuing and processing.
func tryGetLatestObject(informer cache.SharedIndexInformer, obj interface{}) interface{} {
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
	if !c.serviceInformer.HasSynced() ||
		!c.endpoints.HasSynced() ||
		!c.pods.informer.HasSynced() ||
		!c.nodeInformer.HasSynced() {
		return false
	}

	// after informer caches sync the first time, process resources in order
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if !c.initialSync {
		if err := c.SyncAll(); err != nil {
			log.Errorf("one or more errors force-syncing resources: %v", err)
		}
		c.initialSync = true
	}

	return true
}

func (c *Controller) SyncAll() error {
	var err *multierror.Error

	nodes := c.nodeInformer.GetStore().List()
	log.Debugf("initialzing %d nodes", len(nodes))
	for _, s := range nodes {
		err = multierror.Append(err, c.onNodeEvent(s, model.EventAdd))
	}

	services := c.serviceInformer.GetStore().List()
	log.Debugf("initialzing %d services", len(services))
	for _, s := range services {
		err = multierror.Append(err, c.onServiceEvent(s, model.EventAdd))
	}

	err = multierror.Append(err, c.syncPods())
	err = multierror.Append(err, c.syncEndpoints())

	return multierror.Flatten(err.ErrorOrNil())
}

func (c *Controller) syncPods() error {
	var err *multierror.Error
	pods := c.pods.informer.GetStore().List()
	log.Debugf("initialzing %d pods", len(pods))
	for _, s := range pods {
		err = multierror.Append(err, c.pods.onEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

func (c *Controller) syncEndpoints() error {
	var err *multierror.Error
	endpoints := c.endpoints.getInformer().GetStore().List()
	log.Debugf("initialzing%d endpoints", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, c.endpoints.onEvent(s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	if c.networksWatcher != nil {
		c.networksWatcher.AddNetworksHandler(c.reloadNetworkLookup)
		c.reloadNetworkLookup()
	}

	go func() {
		cache.WaitForCacheSync(stop, c.HasSynced)
		c.queue.Run(stop)
	}()

	// To avoid endpoints without labels or ports, wait for sync.
	cache.WaitForCacheSync(stop, c.nodeInformer.HasSynced,
		c.pods.informer.HasSynced,
		c.serviceInformer.HasSynced)

	<-stop
	log.Infof("Controller terminated")
}

// Stop the controller. Only for tests, to simplify the code (defer c.Stop())
func (c *Controller) Stop() {
	if c.stop != nil {
		close(c.stop)
	}
}

// Services implements a service catalog operation
func (c *Controller) Services() ([]*model.Service, error) {
	c.RLock()
	out := make([]*model.Service, 0, len(c.servicesMap))
	for _, svc := range c.servicesMap {
		out = append(out, svc)
	}
	c.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })

	return out, nil
}

// GetService implements a service catalog operation by hostname specified.
func (c *Controller) GetService(hostname host.Name) (*model.Service, error) {
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()
	return svc, nil
}

// getNodePortServices returns nodePort type gateway service
func (c *Controller) getNodePortGatewayServices() []*model.Service {
	c.RLock()
	defer c.RUnlock()
	out := make([]*model.Service, 0, len(c.nodeSelectorsForServices))
	for hostname := range c.nodeSelectorsForServices {
		svc := c.servicesMap[hostname]
		if svc != nil {
			out = append(out, svc)
		}
	}

	return out
}

// updateServiceExternalAddr updates ClusterExternalAddresses for ingress gateway service of nodePort type
func (c *Controller) updateServiceExternalAddr(svcs ...*model.Service) bool {
	// node event, update all nodePort gateway services
	if len(svcs) == 0 {
		svcs = c.getNodePortGatewayServices()
	}
	// no nodePort gateway service found, no update
	if len(svcs) == 0 {
		return false
	}
	for _, svc := range svcs {
		c.RLock()
		nodeSelector := c.nodeSelectorsForServices[svc.Hostname]
		c.RUnlock()
		// update external address
		svc.Mutex.Lock()
		if nodeSelector == nil {
			var extAddresses []string
			for _, n := range c.nodeInfoMap {
				extAddresses = append(extAddresses, n.address)
			}
			svc.Attributes.ClusterExternalAddresses = map[string][]string{c.clusterID: extAddresses}
		} else {
			var nodeAddresses []string
			for _, n := range c.nodeInfoMap {
				if nodeSelector.SubsetOf(n.labels) {
					nodeAddresses = append(nodeAddresses, n.address)
				}
			}
			svc.Attributes.ClusterExternalAddresses = map[string][]string{c.clusterID: nodeAddresses}
		}
		svc.Mutex.Unlock()
	}
	return true
}

// getPodLocality retrieves the locality for a pod.
func (c *Controller) getPodLocality(pod *v1.Pod) string {
	// if pod has `istio-locality` label, skip below ops
	if len(pod.Labels[model.LocalityLabel]) > 0 {
		return model.GetLocalityLabelOrDefault(pod.Labels[model.LocalityLabel], "")
	}

	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#late-initialization
	raw, err := c.nodeLister.Get(pod.Spec.NodeName)
	if err != nil {
		log.Warnf("unable to get node %q for pod %q: %v", pod.Spec.NodeName, pod.Name, err)
		return ""
	}

	nodeMeta, err := meta.Accessor(raw)
	if err != nil {
		log.Warnf("unable to get node meta: %v", nodeMeta)
		return ""
	}

	region := getLabelValue(nodeMeta, NodeRegionLabel, NodeRegionLabelGA)
	zone := getLabelValue(nodeMeta, NodeZoneLabel, NodeZoneLabelGA)
	subzone := getLabelValue(nodeMeta, IstioSubzoneLabel, "")

	if region == "" && zone == "" && subzone == "" {
		return ""
	}

	return region + "/" + zone + "/" + subzone // Format: "%s/%s/%s"
}

// InstancesByPort implements a service catalog operation
func (c *Controller) InstancesByPort(svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	// First get k8s standard service instances and the workload entry instances
	outInstances, err := c.endpoints.InstancesByPort(c, svc, reqSvcPort, labelsList)
	outInstances = append(outInstances, c.serviceInstancesFromWorkloadInstances(svc, reqSvcPort)...)

	// return when instances found or an error occurs
	if len(outInstances) > 0 || err != nil {
		return outInstances, err
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
		return inScopeInstances, nil
	}
	return nil, nil
}

func (c *Controller) serviceInstancesFromWorkloadInstances(svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	// Run through all the workload instances, select ones that match the service labels
	// only if this is a kubernetes internal service and of ClientSideLB (eds) type
	// as InstancesByPort is called by the aggregate controller. We dont want to include
	// workload instances for any other registry
	var workloadInstancesExist bool
	c.RLock()
	workloadInstancesExist = len(c.workloadInstancesByIP) > 0
	c.RUnlock()

	// Only select internal Kubernetes services with selectors
	if !workloadInstancesExist || svc.Attributes.ServiceRegistry != string(serviceregistry.Kubernetes) ||
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
	targetPort, targetPortName := findServiceTargetPort(servicePort, k8sService)
	if targetPort == 0 {
		targetPort = reqSvcPort
	}

	out := make([]*model.ServiceInstance, 0)

	c.RLock()
	for _, wi := range c.workloadInstancesByIP {
		if wi.Namespace != svc.Attributes.Namespace {
			continue
		}
		if selector.SubsetOf(wi.Endpoint.Labels) {
			// create an instance with endpoint whose service port name matches
			istioEndpoint := *wi.Endpoint
			if targetPortName != "" {
				// This is a named port, find the corresponding port in the port map
				matchedPort := wi.PortMap[targetPortName]
				if matchedPort == 0 {
					// No match found, skip this endpoint
					continue
				}
				istioEndpoint.EndpointPort = matchedPort
			} else {
				istioEndpoint.EndpointPort = uint32(targetPort)
			}
			istioEndpoint.ServicePortName = servicePort.Name
			out = append(out, &model.ServiceInstance{
				Service:     svc,
				ServicePort: servicePort,
				Endpoint:    &istioEndpoint,
			})
		}
	}
	c.RUnlock()
	return out
}

// convenience function to collect all workload entry endpoints in updateEDS calls.
func (c *Controller) collectWorkloadInstanceEndpoints(svc *model.Service) []*model.IstioEndpoint {
	var workloadInstancesExist bool
	c.RLock()
	workloadInstancesExist = len(c.workloadInstancesByIP) > 0
	c.RUnlock()

	if !workloadInstancesExist || svc.Resolution != model.ClientSideLB || len(svc.Ports) == 0 {
		return nil
	}

	instances := c.serviceInstancesFromWorkloadInstances(svc, svc.Ports[0].Port)
	endpoints := make([]*model.IstioEndpoint, 0)

	// all endpoints for ports[0]
	for _, instance := range instances {
		endpoints = append(endpoints, instance.Endpoint)
	}

	// build an endpoint for each remaining service port
	for i := 1; i < len(svc.Ports); i++ {
		for _, instance := range instances {
			ep := *instance.Endpoint
			ep.EndpointPort = uint32(svc.Ports[i].Port)
			ep.ServicePortName = svc.Ports[i].Name
			endpoints = append(endpoints, &ep)
		}
	}
	return endpoints
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
// TODO: this code does not return k8s service instances when the proxy's IP is a workload entry
// To tackle this, we need a ip2instance map like what we have in service entry.
func (c *Controller) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	if len(proxy.IPAddresses) > 0 {
		// only need to fetch the corresponding pod through the first IP, although there are multiple IP scenarios,
		// because multiple ips belong to the same pod
		proxyIP := proxy.IPAddresses[0]

		pod := c.pods.getPodByIP(proxyIP)
		if workload, f := c.workloadInstancesByIP[proxyIP]; f {
			var err error
			out, err := c.hydrateWorkloadInstance(workload)
			if err != nil {
				log.Warnf("hydrateWorkloadInstance for %v failed: %v", proxy.ID, err)
			}
			return out, nil
		} else if pod != nil {
			if !c.isControllerForProxy(proxy) {
				return nil, fmt.Errorf("proxy is in cluster %v, but controller is for cluster %v", proxy.Metadata.ClusterID, c.clusterID)
			}

			// 1. find proxy service by label selector, if not any, there may exist headless service without selector
			// failover to 2
			if services, err := getPodServices(c.serviceLister, pod); err == nil && len(services) > 0 {
				out := make([]*model.ServiceInstance, 0)
				for _, svc := range services {
					out = append(out, c.getProxyServiceInstancesByPod(pod, svc, proxy)...)
				}
				return out, nil
			}
			// 2. Headless service without selector
			return c.endpoints.GetProxyServiceInstances(c, proxy), nil
		} else {
			var err error
			// 3. The pod is not present when this is called
			// due to eventual consistency issues. However, we have a lot of information about the pod from the proxy
			// metadata already. Because of this, we can still get most of the information we need.
			// If we cannot accurately construct ServiceInstances from just the metadata, this will return an error and we can
			// attempt to read the real pod.
			out, err := c.getProxyServiceInstancesFromMetadata(proxy)
			if err != nil {
				log.Warnf("getProxyServiceInstancesFromMetadata for %v failed: %v", proxy.ID, err)
			}
			return out, nil
		}
	}
	if c.metrics != nil {
		c.metrics.AddMetric(model.ProxyStatusNoService, proxy.ID, proxy, "")
	} else {
		log.Infof("Missing metrics env, empty list of services for pod %s", proxy.ID)
	}
	return nil, nil
}

func (c *Controller) hydrateWorkloadInstance(si *model.WorkloadInstance) ([]*model.ServiceInstance, error) {
	out := []*model.ServiceInstance{}
	// find the workload entry's service by label selector
	// rather than scanning through our internal map of model.services, get the services via the k8s apis
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: si.Namespace, Labels: si.Endpoint.Labels},
	}

	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	if k8sServices, err := getPodServices(c.serviceLister, dummyPod); err == nil && len(k8sServices) > 0 {
		for _, k8sSvc := range k8sServices {
			var service *model.Service
			c.RLock()
			service = c.servicesMap[kube.ServiceHostname(k8sSvc.Name, k8sSvc.Namespace, c.domainSuffix)]
			c.RUnlock()
			// Note that this cannot be an external service because k8s external services do not have label selectors.
			if service == nil || service.Resolution != model.ClientSideLB {
				// may be a headless service
				continue
			}

			for _, port := range service.Ports {
				if port.Protocol == protocol.UDP {
					continue
				}
				// Similar code as UpdateServiceShards in eds.go
				instances, err := c.InstancesByPort(service, port.Port, labels.Collection{})
				if err != nil {
					return nil, err
				}
				out = append(out, instances...)
			}
		}
	}
	return out, nil
}

// WorkloadInstanceHandler defines the handler for service instances generated by other registries
func (c *Controller) WorkloadInstanceHandler(si *model.WorkloadInstance, event model.Event) {
	// ignore malformed workload entries. And ignore any workload entry that does not have a label
	// as there is no way for us to select them
	if si.Namespace == "" || len(si.Endpoint.Labels) == 0 {
		return
	}

	// this is from a workload entry. Store it in separate map so that
	// the InstancesByPort can use these as well as the k8s pods.
	c.Lock()
	switch event {
	case model.EventDelete:
		delete(c.workloadInstancesByIP, si.Endpoint.Address)
	default: // add or update
		// Check to see if the workload entry changed. If it did, clear the old entry
		k := si.Name + "~" + si.Namespace
		existing := c.workloadInstancesIPsByName[k]
		if existing != si.Endpoint.Address {
			delete(c.workloadInstancesByIP, existing)
		}
		c.workloadInstancesByIP[si.Endpoint.Address] = si
		c.workloadInstancesIPsByName[k] = si.Endpoint.Address
	}
	c.Unlock()

	// find the workload entry's service by label selector
	// rather than scanning through our internal map of model.services, get the services via the k8s apis
	dummyPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: si.Namespace, Labels: si.Endpoint.Labels},
	}

	// find the services that map to this workload entry, fire off eds updates if the service is of type client-side lb
	if k8sServices, err := getPodServices(c.serviceLister, dummyPod); err == nil && len(k8sServices) > 0 {
		for _, k8sSvc := range k8sServices {
			var service *model.Service
			c.RLock()
			service = c.servicesMap[kube.ServiceHostname(k8sSvc.Name, k8sSvc.Namespace, c.domainSuffix)]
			c.RUnlock()
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
				instances, err := c.InstancesByPort(service, port.Port, labels.Collection{})
				if err != nil {
					log.Debugf("Failed to get endpoints for service %s on port %d, in response to workload instance: %v",
						service.Hostname, port.Port, err)
					continue
				}

				for _, inst := range instances {
					endpoints = append(endpoints, inst.Endpoint)
				}
			}
			// fire off eds update
			_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(service.Hostname), service.Attributes.Namespace, endpoints)
		}
	}
}

// isControllerForProxy should be used for proxies assumed to be in the kube cluster for this controller. Workload Entries
// may not necessarily pass this check, but we still want to allow kube services to select workload instances.
func (c *Controller) isControllerForProxy(proxy *model.Proxy) bool {
	return proxy.Metadata.ClusterID == c.clusterID
}

// getProxyServiceInstancesFromMetadata retrieves ServiceInstances using proxy Metadata rather than
// from the Pod. This allows retrieving Instances immediately, regardless of delays in Kubernetes.
// If the proxy doesn't have enough metadata, an error is returned
func (c *Controller) getProxyServiceInstancesFromMetadata(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	if len(proxy.Metadata.Labels) == 0 {
		return nil, fmt.Errorf("no workload labels found")
	}

	if !c.isControllerForProxy(proxy) {
		return nil, fmt.Errorf("proxy is in cluster %v, but controller is for cluster %v", proxy.Metadata.ClusterID, c.clusterID)
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
		svcAccount := proxy.Metadata.ServiceAccount
		hostname := kube.ServiceHostname(svc.Name, svc.Namespace, c.domainSuffix)
		c.RLock()
		modelService, f := c.servicesMap[hostname]
		c.RUnlock()
		if !f {
			return nil, fmt.Errorf("failed to find model service for %v", hostname)
		}

		tps := make(map[model.Port]*model.Port)
		for _, port := range svc.Spec.Ports {
			svcPort, f := modelService.Ports.Get(port.Name)
			if !f {
				return nil, fmt.Errorf("failed to get svc port for %v", port.Name)
			}
			portNum, err := findPortFromMetadata(port, proxy.Metadata.PodPorts)
			if err != nil {
				return nil, fmt.Errorf("failed to find target port for %v: %v", proxy.ID, err)
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
			}
		}

		for tp, svcPort := range tps {
			// consider multiple IP scenarios
			for _, ip := range proxy.IPAddresses {
				// Construct the ServiceInstance
				out = append(out, &model.ServiceInstance{
					Service:     modelService,
					ServicePort: svcPort,
					Endpoint: &model.IstioEndpoint{
						Address:         ip,
						EndpointPort:    uint32(tp.Port),
						ServicePortName: svcPort.Name,
						// Kubernetes service will only have a single instance of labels, and we return early if there are no labels.
						Labels:         proxy.Metadata.Labels,
						ServiceAccount: svcAccount,
						Network:        c.endpointNetwork(ip),
						Locality: model.Locality{
							Label:     util.LocalityToString(proxy.Locality),
							ClusterID: c.clusterID,
						},
					},
				})
			}
		}
	}
	return out, nil
}

func (c *Controller) getProxyServiceInstancesByPod(pod *v1.Pod, service *v1.Service, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(service.Name, service.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc == nil {
		return out
	}

	tps := make(map[model.Port]*model.Port)
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
		if _, exists = tps[targetPort]; !exists {
			tps[targetPort] = svcPort
		}
	}

	builder := NewEndpointBuilder(c, pod)
	for tp, svcPort := range tps {
		// consider multiple IP scenarios
		for _, ip := range proxy.IPAddresses {
			istioEndpoint := builder.buildIstioEndpoint(ip, int32(tp.Port), svcPort.Name)
			out = append(out, &model.ServiceInstance{
				Service:     svc,
				ServicePort: svcPort,
				Endpoint:    istioEndpoint,
			})
		}
	}
	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	pod := c.pods.getPodByIP(proxyIP)
	if pod != nil {
		return labels.Collection{pod.Labels}, nil
	}
	return nil, nil
}

// GetIstioServiceAccounts returns the Istio service accounts running a serivce
// hostname. Each service account is encoded according to the SPIFFE VSID spec.
// For example, a service account named "bar" in namespace "foo" is encoded as
// "spiffe://cluster.local/ns/foo/sa/bar".
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	return model.GetServiceAccounts(svc, ports, c)
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

// AppendWorkloadHandler implements a service catalog operation
func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error {
	c.workloadHandlers = append(c.workloadHandlers, f)
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}
