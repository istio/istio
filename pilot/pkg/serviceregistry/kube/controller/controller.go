// Copyright 2017 Istio Authors
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/yl2chen/cidranger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/queue"
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
	// IstioConfigMap is used by default
	IstioConfigMap = "istio"
	// PrometheusScrape is the annotation used by prometheus to determine if service metrics should be scraped (collected)
	PrometheusScrape = "prometheus.io/scrape"
	// PrometheusPort is the annotation used to explicitly specify the port to use for scraping metrics
	PrometheusPort = "prometheus.io/port"
	// PrometheusPath is the annotation used to specify a path for scraping metrics. Default is "/metrics"
	PrometheusPath = "prometheus.io/path"
	// PrometheusPathDefault is the default value for the PrometheusPath annotation
	PrometheusPathDefault = "/metrics"
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
	WatchedNamespace string
	ResyncPeriod     time.Duration
	DomainSuffix     string

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

	//CABundlePath defines the caBundle path for istiod Server
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
	client         kubernetes.Interface
	metadataClient metadata.Interface
	queue          queue.Instance
	services       cache.SharedIndexInformer
	endpoints      kubeEndpointsController

	nodeMetadataInformer cache.SharedIndexInformer
	// Used to watch node accessible from remote cluster.
	// In multi-cluster(shared control plane multi-networks) scenario, ingress gateway service can be of nodePort type.
	// With this, we can populate mesh's gateway address with the node ips.
	filteredNodeInformer cache.SharedIndexInformer
	pods                 *PodCache
	metrics              model.Metrics
	networksWatcher      mesh.NetworksWatcher
	xdsUpdater           model.XDSUpdater
	domainSuffix         string
	clusterID            string

	serviceHandlers  []func(*model.Service, model.Event)
	instanceHandlers []func(*model.ServiceInstance, model.Event)

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
	nodeInfoMap map[string]kubernetesNode
	// externalNameSvcInstanceMap stores hostname ==> instance, is used to store instances for ExternalName k8s services
	externalNameSvcInstanceMap map[host.Name][]*model.ServiceInstance

	// CIDR ranger based on path-compressed prefix trie
	ranger cidranger.Ranger

	// Network name for the registry as specified by the MeshNetworks configmap
	networkForRegistry string
}

// NewController creates a new Kubernetes controller
// Created by bootstrap and multicluster (see secretcontroler).
func NewController(client kubernetes.Interface, metadataClient metadata.Interface, options Options) *Controller {
	log.Infof("Service controller watching namespace %q for services, endpoints, nodes and pods, refresh %s",
		options.WatchedNamespace, options.ResyncPeriod)

	// The queue requires a time duration for a retry delay after a handler error
	c := &Controller{
		domainSuffix:               options.DomainSuffix,
		client:                     client,
		metadataClient:             metadataClient,
		queue:                      queue.NewQueue(1 * time.Second),
		clusterID:                  options.ClusterID,
		xdsUpdater:                 options.XDSUpdater,
		servicesMap:                make(map[host.Name]*model.Service),
		nodeSelectorsForServices:   make(map[host.Name]labels.Instance),
		nodeInfoMap:                make(map[string]kubernetesNode),
		externalNameSvcInstanceMap: make(map[host.Name][]*model.ServiceInstance),
		networksWatcher:            options.NetworksWatcher,
		metrics:                    options.Metrics,
	}

	sharedInformers := informers.NewSharedInformerFactoryWithOptions(client, options.ResyncPeriod, informers.WithNamespace(options.WatchedNamespace))

	c.services = sharedInformers.Core().V1().Services().Informer()
	registerHandlers(c.services, c.queue, "Services", c.onServiceEvent)

	switch options.EndpointMode {
	case EndpointsOnly:
		c.endpoints = newEndpointsController(c, sharedInformers)
	case EndpointSliceOnly:
		c.endpoints = newEndpointSliceController(c, sharedInformers)
	}

	// This is for getting the pod to node mapping, so that we can get the pod's locality.
	metadataSharedInformer := metadatainformer.NewSharedInformerFactory(metadataClient, options.ResyncPeriod)
	nodeResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
	c.nodeMetadataInformer = metadataSharedInformer.ForResource(nodeResource).Informer()
	// This is for getting the node IPs of a selected set of nodes
	c.filteredNodeInformer = coreinformers.NewFilteredNodeInformer(client, options.ResyncPeriod,
		cache.Indexers{},
		func(options *metav1.ListOptions) {})
	registerHandlers(c.filteredNodeInformer, c.queue, "Nodes", c.onNodeEvent)

	podInformer := sharedInformers.Core().V1().Pods().Informer()
	c.pods = newPodCache(podInformer, c, func(key string) {
		item, exists, err := c.endpoints.getInformer().GetStore().GetByKey(key)
		if err != nil || !exists {
			log.Debugf("Endpoint %v lookup failed, skipping stale endpoint. error: %v", key, err)
			return
		}
		c.queue.Push(func() error {
			return c.endpoints.onEvent(item, model.EventUpdate)
		})
	})
	registerHandlers(podInformer, c.queue, "Pods", c.pods.onEvent)

	return c
}

func (c *Controller) Provider() serviceregistry.ProviderID {
	return serviceregistry.Kubernetes
}

func (c *Controller) Cluster() string {
	return c.clusterID
}

func (c *Controller) checkReadyForEvents() error {
	if !c.HasSynced() {
		return errors.New("waiting till full synchronization")
	}
	return nil
}

func (c *Controller) onServiceEvent(curr interface{}, event model.Event) error {
	if err := c.checkReadyForEvents(); err != nil {
		return err
	}

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
		// instance conversion is only required when service is added/updated.
		instances := kube.ExternalNameServiceInstances(*svc, svcConv)
		if isNodePortGatewayService(svc) {
			// We need to know which services are using node selectors because during node events,
			// we have to update all the node port services accordingly.
			nodeSelector := getNodeSelectorsForService(*svc)
			c.Lock()
			// only add when it is nodePort gateway service
			c.nodeSelectorsForServices[svcConv.Hostname] = nodeSelector
			c.Unlock()
			c.updateServiceExternalAddr(svcConv)
		}
		c.Lock()
		c.servicesMap[svcConv.Hostname] = svcConv
		if len(instances) > 0 {
			c.externalNameSvcInstanceMap[svcConv.Hostname] = instances
		}
		c.Unlock()
	}

	c.xdsUpdater.SvcUpdate(c.clusterID, svc.Name, svc.Namespace, event)
	// Notify service handlers.
	for _, f := range c.serviceHandlers {
		f(svcConv, event)
	}

	return nil
}

func getNodeSelectorsForService(svc v1.Service) labels.Instance {
	if nodeSelector := svc.Annotations[kube.NodeSelectorAnnotation]; nodeSelector != "" {
		var nodeSelectorKV map[string]string
		if err := json.Unmarshal([]byte(nodeSelector), &nodeSelectorKV); err != nil {
			log.Debugf("failed to unmarshal node selector annotation value for service %s.%s: %v",
				svc.Name, svc.Namespace, err)
		}
		return nodeSelectorKV
	}
	return nil
}

func (c *Controller) onNodeEvent(obj interface{}, event model.Event) error {
	if err := c.checkReadyForEvents(); err != nil {
		return err
	}
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
		if !exists || !reflect.DeepEqual(currentNode, k8sNode) {
			c.nodeInfoMap[node.Name] = k8sNode
			updatedNeeded = true
		}
		c.Unlock()
	}

	if updatedNeeded {
		// update all related services
		c.updateServiceExternalAddr()
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full: true,
		})
	}
	return nil
}

func isNodePortGatewayService(svc *v1.Service) bool {
	_, ok := svc.Annotations[kube.NodeSelectorAnnotation]
	return ok && svc.Spec.Type == v1.ServiceTypeNodePort
}

func registerHandlers(informer cache.SharedIndexInformer, q queue.Instance, otype string,
	handler func(interface{}, model.Event) error) {

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				incrementEvent(otype, "add")
				q.Push(func() error {
					return handler(obj, model.EventAdd)
				})
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					incrementEvent(otype, "update")
					q.Push(func() error {
						return handler(cur, model.EventUpdate)
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

// compareEndpoints returns true if the two endpoints are the same in aspects Pilot cares about
// This currently means only looking at "Ready" endpoints
func compareEndpoints(a, b *v1.Endpoints) bool {
	if len(a.Subsets) != len(b.Subsets) {
		return false
	}
	for i := range a.Subsets {
		if !reflect.DeepEqual(a.Subsets[i].Ports, b.Subsets[i].Ports) {
			return false
		}
		if !reflect.DeepEqual(a.Subsets[i].Addresses, b.Subsets[i].Addresses) {
			return false
		}
	}
	return true
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	if !c.services.HasSynced() ||
		!c.endpoints.HasSynced() ||
		!c.pods.informer.HasSynced() ||
		!c.nodeMetadataInformer.HasSynced() ||
		!c.filteredNodeInformer.HasSynced() {
		return false
	}
	return true
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	if c.networksWatcher != nil {
		c.networksWatcher.AddNetworksHandler(c.initNetworkLookup)
		c.initNetworkLookup()
	}

	go func() {
		cache.WaitForCacheSync(stop, c.HasSynced)
		c.queue.Run(stop)
	}()

	go c.services.Run(stop)
	go c.pods.informer.Run(stop)
	go c.nodeMetadataInformer.Run(stop)
	go c.filteredNodeInformer.Run(stop)

	// To avoid endpoints without labels or ports, wait for sync.
	cache.WaitForCacheSync(stop, c.nodeMetadataInformer.HasSynced, c.filteredNodeInformer.HasSynced,
		c.pods.informer.HasSynced,
		c.services.HasSynced)

	go c.endpoints.Run(stop)

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
func (c *Controller) updateServiceExternalAddr(svcs ...*model.Service) {
	// node event, update all nodePort gateway services
	if len(svcs) == 0 {
		svcs = c.getNodePortGatewayServices()
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
}

// getPodLocality retrieves the locality for a pod.
func (c *Controller) getPodLocality(pod *v1.Pod) string {
	// if pod has `istio-locality` label, skip below ops
	if len(pod.Labels[model.LocalityLabel]) > 0 {
		return model.GetLocalityLabelOrDefault(pod.Labels[model.LocalityLabel], "")
	}

	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#late-initialization
	raw, exists, err := c.nodeMetadataInformer.GetStore().GetByKey(pod.Spec.NodeName)
	if !exists || err != nil {
		log.Warnf("unable to get node %q for pod %q from cache: %v", pod.Spec.NodeName, pod.Name, err)
		nodeResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
		raw, err = c.metadataClient.Resource(nodeResource).Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Warnf("unable to get node %q for pod %q: %v", pod.Spec.NodeName, pod.Name, err)
			return ""
		}
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

// ManagementPorts implements a service catalog operation
func (c *Controller) ManagementPorts(addr string) model.PortList {
	pod := c.pods.getPodByIP(addr)
	if pod == nil {
		return nil
	}

	managementPorts, err := kube.ConvertProbesToPorts(&pod.Spec)
	if err != nil {
		log.Infof("Error while parsing liveliness and readiness probe ports for %s => %v", addr, err)
	}

	// We continue despite the error because healthCheckPorts could return a partial
	// list of management ports
	return managementPorts
}

// WorkloadHealthCheckInfo implements a service catalog operation
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	pod := c.pods.getPodByIP(addr)
	if pod == nil {
		return nil
	}

	probes := make([]*model.Probe, 0)

	// Obtain probes from the readiness and liveness probes
	for _, container := range pod.Spec.Containers {
		if container.ReadinessProbe != nil && container.ReadinessProbe.Handler.HTTPGet != nil {
			p, err := kube.ConvertProbePort(&container, &container.ReadinessProbe.Handler)
			if err != nil {
				log.Infof("Error while parsing readiness probe port =%v", err)
			}
			probes = append(probes, &model.Probe{
				Port: p,
				Path: container.ReadinessProbe.Handler.HTTPGet.Path,
			})
		}
		if container.LivenessProbe != nil && container.LivenessProbe.Handler.HTTPGet != nil {
			p, err := kube.ConvertProbePort(&container, &container.LivenessProbe.Handler)
			if err != nil {
				log.Infof("Error while parsing liveness probe port =%v", err)
			}
			probes = append(probes, &model.Probe{
				Port: p,
				Path: container.LivenessProbe.Handler.HTTPGet.Path,
			})
		}
	}

	// Obtain probe from prometheus scrape
	if scrape := pod.Annotations[PrometheusScrape]; scrape == "true" {
		var port *model.Port
		path := PrometheusPathDefault
		if portstr := pod.Annotations[PrometheusPort]; portstr != "" {
			portnum, err := strconv.Atoi(portstr)
			if err != nil {
				log.Warna(err)
			} else {
				port = &model.Port{
					Port: portnum,
				}
			}
		}
		if pod.Annotations[PrometheusPath] != "" {
			path = pod.Annotations[PrometheusPath]
		}
		probes = append(probes, &model.Probe{
			Port: port,
			Path: path,
		})
	}

	return probes
}

// InstancesByPort implements a service catalog operation
func (c *Controller) InstancesByPort(svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	res, err := c.endpoints.InstancesByPort(c, svc, reqSvcPort, labelsList)
	// return when instances found or an error occurs
	if len(res) > 0 || err != nil {
		return res, err
	}

	// Fall back to external name service
	c.RLock()
	instances := c.externalNameSvcInstanceMap[svc.Hostname]
	c.RUnlock()
	if instances != nil {
		inScopeInstances := make([]*model.ServiceInstance, 0)
		for _, i := range instances {
			if i.Service.Attributes.Namespace == svc.Attributes.Namespace && i.ServicePort.Port == reqSvcPort {
				inScopeInstances = append(inScopeInstances, i)
			}
		}
		return inScopeInstances, nil
	}
	return nil, nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)
	if len(proxy.IPAddresses) > 0 {
		// only need to fetch the corresponding pod through the first IP, although there are multiple IP scenarios,
		// because multiple ips belong to the same pod
		proxyIP := proxy.IPAddresses[0]
		pod := c.pods.getPodByIP(proxyIP)
		if pod != nil {
			// for split horizon EDS k8s multi cluster, in case there are pods of the same ip across clusters,
			// which can happen when multi clusters using same pod cidr.
			// As we have proxy Network meta, compare it with the network which endpoint belongs to,
			// if they are not same, ignore the pod, because the pod is in another cluster.
			if proxy.Metadata.Network != c.endpointNetwork(proxyIP) {
				return out, nil
			}
			// 1. find proxy service by label selector, if not any, there may exist headless service without selector
			// failover to 2
			if services, err := getPodServices(listerv1.NewServiceLister(c.services.GetIndexer()), pod); err == nil && len(services) > 0 {
				for _, svc := range services {
					out = append(out, c.getProxyServiceInstancesByPod(pod, svc, proxy)...)
				}
				return out, nil
			}
			// 2. Headless service without selector
			out = c.endpoints.GetProxyServiceInstances(c, proxy)
		} else {
			var err error
			// 3. The pod is not present when this is called
			// due to eventual consistency issues. However, we have a lot of information about the pod from the proxy
			// metadata already. Because of this, we can still get most of the information we need.
			// If we cannot accurately construct ServiceInstances from just the metadata, this will return an error and we can
			// attempt to read the real pod.
			out, err = c.getProxyServiceInstancesFromMetadata(proxy)
			if err != nil {
				log.Warnf("getProxyServiceInstancesFromMetadata for %v failed: %v", proxy.ID, err)
			}
		}
	}
	if len(out) == 0 {
		if c.metrics != nil {
			c.metrics.AddMetric(model.ProxyStatusNoService, proxy.ID, proxy, "")
		} else {
			log.Infof("Missing metrics env, empty list of services for pod %s", proxy.ID)
		}
	}
	return out, nil
}

func getPodServices(s listerv1.ServiceLister, pod *v1.Pod) ([]*v1.Service, error) {
	allServices, err := s.Services(pod.Namespace).List(klabels.Everything())
	if err != nil {
		return nil, err
	}

	var services []*v1.Service
	for i := range allServices {
		service := allServices[i]
		if service.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			continue
		}
		selector := klabels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(klabels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}

	return services, nil
}

// getProxyServiceInstancesFromMetadata retrieves ServiceInstances using proxy Metadata rather than
// from the Pod. This allows retrieving Instances immediately, regardless of delays in Kubernetes.
// If the proxy doesn't have enough metadata, an error is returned
func (c *Controller) getProxyServiceInstancesFromMetadata(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	if len(proxy.Metadata.Labels) == 0 {
		return nil, fmt.Errorf("no workload labels found")
	}

	if proxy.Metadata.ClusterID != c.clusterID {
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
	svcLister := listerv1.NewServiceLister(c.services.GetIndexer())
	services, err := getPodServices(svcLister, dummyPod)
	if err != nil {
		return nil, fmt.Errorf("error getting instances: %v", err)

	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no instances found: %v ", err)
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

// findPortFromMetadata resolves the TargetPort of a Service Port, by reading the Pod spec.
func findPortFromMetadata(svcPort v1.ServicePort, podPorts []model.PodPort) (int, error) {
	target := svcPort.TargetPort

	switch target.Type {
	case intstr.String:
		name := target.StrVal
		for _, port := range podPorts {
			if port.Name == name {
				return port.ContainerPort, nil
			}
		}
	case intstr.Int:
		// For a direct reference we can just return the port number
		return target.IntValue(), nil
	}

	return 0, fmt.Errorf("no matching port found for %+v", svcPort)
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
	saSet := make(map[string]bool)

	instances := make([]*model.ServiceInstance, 0)
	// Get the service accounts running service within Kubernetes. This is reflected by the pods that
	// the service is deployed on, and the service accounts of the pods.
	for _, port := range ports {
		svcInstances, err := c.InstancesByPort(svc, port, labels.Collection{})
		if err != nil {
			log.Warnf("InstancesByPort(%s:%d) error: %v", svc.Hostname, port, err)
			return nil
		}
		instances = append(instances, svcInstances...)
	}

	for _, si := range instances {
		if si.Endpoint.ServiceAccount != "" {
			saSet[si.Endpoint.ServiceAccount] = true
		}
	}

	for _, serviceAccount := range svc.ServiceAccounts {
		sa := serviceAccount
		saSet[sa] = true
	}

	saArray := make([]string, 0, len(saSet))
	for sa := range saSet {
		saArray = append(saArray, sa)
	}

	return saArray
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

// getPod fetches a pod by IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod. In this case, expectPod will be false.
// * It is an endpoint with an associate Pod, but its not found. In this case, expectPod will be true.
//   this may happen due to eventually consistency issues, out of order events, etc. In this case, the caller
//   should not precede with the endpoint, or inaccurate information would be sent which may have impacts on
//   correctness and security.
func getPod(c *Controller, ip string, ep *metav1.ObjectMeta, targetRef *v1.ObjectReference, host host.Name) (rpod *v1.Pod, expectPod bool) {
	pod := c.pods.getPodByIP(ip)
	if pod != nil {
		return pod, false
	}
	// This means, the endpoint event has arrived before pod event.
	// This might happen because PodCache is eventually consistent.
	if targetRef != nil && targetRef.Kind == "Pod" {
		key := kube.KeyFunc(targetRef.Name, targetRef.Namespace)
		// There is a small chance getInformer may have the pod, but it hasn't
		// made its way to the PodCache yet as it a shared queue.
		podFromInformer, f, err := c.pods.informer.GetStore().GetByKey(key)
		if err != nil || !f {
			log.Debugf("Endpoint without pod %s %s.%s error: %v", ip, ep.Name, ep.Namespace, err)
			endpointsWithNoPods.Increment()
			if c.metrics != nil {
				c.metrics.AddMetric(model.EndpointNoPod, string(host), nil, ip)
			}
			// Tell pod cache we want to queue the endpoint event when this pod arrives.
			epkey := kube.KeyFunc(ep.Name, ep.Namespace)
			c.pods.recordNeedsUpdate(epkey, ip)
			return nil, true
		}
		pod = podFromInformer.(*v1.Pod)
	}
	return pod, false
}

func (c *Controller) updateEDS(ep *v1.Endpoints, event model.Event, epc *endpointsController) {
	hostname := kube.ServiceHostname(ep.Name, ep.Namespace, c.domainSuffix)

	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()
	if svc == nil {
		log.Infof("Handle EDS endpoints: skip updating, service %s/%s has not been populated", ep.Name, ep.Namespace)
		return
	}
	endpoints := make([]*model.IstioEndpoint, 0)
	if event != model.EventDelete {
		for _, ss := range ep.Subsets {
			for _, ea := range ss.Addresses {
				pod, expectedUpdate := getPod(c, ea.IP, &metav1.ObjectMeta{Name: ep.Name, Namespace: ep.Namespace}, ea.TargetRef, hostname)
				if pod == nil && expectedUpdate {
					continue
				}

				builder := NewEndpointBuilder(c, pod)

				// EDS and ServiceEntry use name for service port - ADS will need to
				// map to numbers.
				for _, port := range ss.Ports {
					istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, port.Name)
					endpoints = append(endpoints, istioEndpoint)
				}
			}
		}
	} else {
		epc.forgetEndpoint(ep)
	}

	log.Debugf("Handle EDS: %d endpoints for %s in namespace %s", len(endpoints), ep.Name, ep.Namespace)

	_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(hostname), ep.Namespace, endpoints)
	for _, handler := range c.instanceHandlers {
		for _, ep := range endpoints {
			si := &model.ServiceInstance{
				Service:     svc,
				ServicePort: nil,
				Endpoint:    ep,
			}
			handler(si, event)
		}
	}
}

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    string
	network net.IPNet
}

// returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

// initNetworkLookup will read the mesh networks configuration from the environment
// and initialize CIDR rangers for an efficient network lookup when needed
func (c *Controller) initNetworkLookup() {
	meshNetworks := c.networksWatcher.Networks()
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}

	c.ranger = cidranger.NewPCTrieRanger()

	for n, v := range meshNetworks.Networks {
		for _, ep := range v.Endpoints {
			if ep.GetFromCidr() != "" {
				_, network, err := net.ParseCIDR(ep.GetFromCidr())
				if err != nil {
					log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), n)
					continue
				}
				rangerEntry := namedRangerEntry{
					name:    n,
					network: *network,
				}
				_ = c.ranger.Insert(rangerEntry)
			}
			if ep.GetFromRegistry() != "" && ep.GetFromRegistry() == c.clusterID {
				c.networkForRegistry = n
			}
		}
	}
}

// return the mesh network for the endpoint IP. Empty string if not found.
func (c *Controller) endpointNetwork(endpointIP string) string {
	// If networkForRegistry is set then all endpoints discovered by this registry
	// belong to the configured network so simply return it
	if len(c.networkForRegistry) != 0 {
		return c.networkForRegistry
	}

	// Try to determine the network by checking whether the endpoint IP belongs
	// to any of the configure networks' CIDR ranges
	if c.ranger == nil {
		return ""
	}
	entries, err := c.ranger.ContainingNetworks(net.ParseIP(endpointIP))
	if err != nil {
		log.Errora(err)
		return ""
	}
	if len(entries) == 0 {
		return ""
	}
	if len(entries) > 1 {
		log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
	}

	return (entries[0].(namedRangerEntry)).name
}

// Forked from Kubernetes k8s.io/kubernetes/pkg/api/v1/pod
// FindPort locates the container port for the given pod and portName.  If the
// targetPort is a number, use that.  If the targetPort is a string, look that
// string up in all named ports in all containers in the target pod.  If no
// match is found, fail.
func FindPort(pod *v1.Pod, svcPort *v1.ServicePort) (int, error) {
	portName := svcPort.TargetPort
	switch portName.Type {
	case intstr.String:
		name := portName.StrVal
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.Name == name && port.Protocol == svcPort.Protocol {
					return int(port.ContainerPort), nil
				}
			}
		}
	case intstr.Int:
		return portName.IntValue(), nil
	}

	return 0, fmt.Errorf("no suitable port for manifest: %s", pod.UID)
}

func createUID(podName, namespace string) string {
	return "kubernetes://" + podName + "." + namespace
}
