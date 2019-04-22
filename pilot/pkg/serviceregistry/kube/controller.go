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

package kube

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/yl2chen/cidranger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/log"
)

const (
	// NodeRegionLabel is the well-known label for kubernetes node region
	NodeRegionLabel = "failure-domain.beta.kubernetes.io/region"
	// NodeZoneLabel is the well-known label for kubernetes node zone
	NodeZoneLabel = "failure-domain.beta.kubernetes.io/zone"
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
	// experiment on getting some monitoring on config errors.
	k8sEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pilot_k8s_reg_events",
		Help: "Events from k8s registry.",
	}, []string{"type", "event"})
)

func init() {
	prometheus.MustRegister(k8sEvents)
}

// ControllerOptions stores the configurable attributes of a Controller.
type ControllerOptions struct {
	// Namespace the controller watches. If set to meta_v1.NamespaceAll (""), controller watches all namespaces
	WatchedNamespace string
	ResyncPeriod     time.Duration
	DomainSuffix     string

	// ClusterID identifies the remote cluster in a multicluster env.
	ClusterID string

	// XDSUpdater will push changes to the xDS server.
	XDSUpdater model.XDSUpdater

	// TrustDomain used in SPIFFE identity
	TrustDomain string

	stop chan struct{}
}

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	domainSuffix string

	client    kubernetes.Interface
	queue     Queue
	services  cacheHandler
	endpoints cacheHandler
	nodes     cacheHandler

	pods *PodCache

	// Env is set by server to point to the environment, to allow the controller to
	// use env data and push status. It may be null in tests.
	Env *model.Environment

	// ClusterID identifies the remote cluster in a multicluster env.
	ClusterID string

	// XDSUpdater will push EDS changes to the ADS model.
	XDSUpdater model.XDSUpdater

	stop chan struct{}

	sync.RWMutex
	// servicesMap stores hostname ==> service, it is used to reduce convertService calls.
	servicesMap map[model.Hostname]*model.Service
	// externalNameSvcInstanceMap stores hostname ==> instance, is used to store instances for ExternalName k8s services
	externalNameSvcInstanceMap map[model.Hostname][]*model.ServiceInstance

	// CIDR ranger based on path-compressed prefix trie
	ranger cidranger.Ranger

	// Network name for the registry as specified by the MeshNetworks configmap
	networkForRegistry string
}

type cacheHandler struct {
	informer cache.SharedIndexInformer
	handler  *ChainHandler
}

// NewController creates a new Kubernetes controller
// Created by bootstrap and multicluster (see secretcontroler).
func NewController(client kubernetes.Interface, options ControllerOptions) *Controller {
	log.Infof("Service controller watching namespace %q for services, endpoints, nodes and pods, refresh %s",
		options.WatchedNamespace, options.ResyncPeriod)

	// Queue requires a time duration for a retry delay after a handler error
	out := &Controller{
		domainSuffix:               options.DomainSuffix,
		client:                     client,
		queue:                      NewQueue(1 * time.Second),
		ClusterID:                  options.ClusterID,
		XDSUpdater:                 options.XDSUpdater,
		servicesMap:                make(map[model.Hostname]*model.Service),
		externalNameSvcInstanceMap: make(map[model.Hostname][]*model.ServiceInstance),
	}

	sharedInformers := informers.NewSharedInformerFactoryWithOptions(client, options.ResyncPeriod, informers.WithNamespace(options.WatchedNamespace))

	svcInformer := sharedInformers.Core().V1().Services().Informer()
	out.services = out.createCacheHandler(svcInformer, "Services")

	epInformer := sharedInformers.Core().V1().Endpoints().Informer()
	out.endpoints = out.createEDSCacheHandler(epInformer, "Endpoints")

	nodeInformer := sharedInformers.Core().V1().Nodes().Informer()
	out.nodes = out.createCacheHandler(nodeInformer, "Nodes")

	podInformer := sharedInformers.Core().V1().Pods().Informer()
	out.pods = newPodCache(out.createCacheHandler(podInformer, "Pod"), out)

	return out
}

// notify is the first handler in the handler chain.
// Returning an error causes repeated execution of the entire chain.
func (c *Controller) notify(obj interface{}, event model.Event) error {
	if !c.HasSynced() {
		return errors.New("waiting till full synchronization")
	}
	return nil
}

// createCacheHandler registers handlers for a specific event.
// Current implementation queues the events in queue.go, and the handler is run with
// some throttling.
// Used for Service, Endpoint, Node and Pod.
// See config/kube for CRD events.
// See config/ingress for Ingress objects
func (c *Controller) createCacheHandler(informer cache.SharedIndexInformer, otype string) cacheHandler {
	handler := &ChainHandler{funcs: []Handler{c.notify}}

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				k8sEvents.With(prometheus.Labels{"type": otype, "event": "add"}).Add(1)
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventAdd})
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					k8sEvents.With(prometheus.Labels{"type": otype, "event": "update"}).Add(1)
					c.queue.Push(Task{handler: handler.Apply, obj: cur, event: model.EventUpdate})
				} else {
					k8sEvents.With(prometheus.Labels{"type": otype, "event": "updateSame"}).Add(1)
				}
			},
			DeleteFunc: func(obj interface{}) {
				k8sEvents.With(prometheus.Labels{"type": otype, "event": "delete"}).Add(1)
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventDelete})
			},
		})

	return cacheHandler{informer: informer, handler: handler}
}

func (c *Controller) createEDSCacheHandler(informer cache.SharedIndexInformer, otype string) cacheHandler {
	handler := &ChainHandler{funcs: []Handler{c.notify}}

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				k8sEvents.With(prometheus.Labels{"type": otype, "event": "add"}).Add(1)
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventAdd})
			},
			UpdateFunc: func(old, cur interface{}) {
				// Avoid pushes if only resource version changed (kube-scheduller, cluster-autoscaller, etc)
				oldE := old.(*v1.Endpoints)
				curE := cur.(*v1.Endpoints)

				if !reflect.DeepEqual(oldE.Subsets, curE.Subsets) {
					k8sEvents.With(prometheus.Labels{"type": otype, "event": "update"}).Add(1)
					// c.updateEDS(cur.(*v1.Endpoints))
					c.queue.Push(Task{handler: handler.Apply, obj: cur, event: model.EventUpdate})
				} else {
					k8sEvents.With(prometheus.Labels{"type": otype, "event": "updateSame"}).Add(1)
				}
			},
			DeleteFunc: func(obj interface{}) {
				k8sEvents.With(prometheus.Labels{"type": otype, "event": "delete"}).Add(1)
				// Deleting the endpoints results in an empty set from EDS perspective - only
				// deleting the service should delete the resources. The full sync replaces the
				// maps.
				// c.updateEDS(obj.(*v1.Endpoints))
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventDelete})
			},
		})

	return cacheHandler{informer: informer, handler: handler}
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	if !c.services.informer.HasSynced() ||
		!c.endpoints.informer.HasSynced() ||
		!c.pods.informer.HasSynced() ||
		!c.nodes.informer.HasSynced() {
		return false
	}
	return true
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	go func() {
		if pilot.EnableWaitCacheSync {
			cache.WaitForCacheSync(stop, c.HasSynced)
		}
		c.queue.Run(stop)
	}()

	go c.services.informer.Run(stop)
	go c.pods.informer.Run(stop)
	go c.nodes.informer.Run(stop)

	// To avoid endpoints without labels or ports, wait for sync.
	cache.WaitForCacheSync(stop, c.nodes.informer.HasSynced, c.pods.informer.HasSynced,
		c.services.informer.HasSynced)

	go c.endpoints.informer.Run(stop)

	<-stop
	log.Infof("Controller terminated")
}

// Stop the controller. Mostly for tests, to simplify the code (defer c.Stop())
func (c *Controller) Stop() {
	if c.stop != nil {
		c.stop <- struct{}{}
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
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	c.RLock()
	defer c.RUnlock()
	return c.servicesMap[hostname], nil
}

// serviceByKey retrieves a service by name and namespace
func (c *Controller) serviceByKey(name, namespace string) (*v1.Service, bool) {
	item, exists, err := c.services.informer.GetStore().GetByKey(KeyFunc(name, namespace))
	if err != nil {
		log.Infof("serviceByKey(%s, %s) => error %v", name, namespace, err)
		return nil, false
	}
	if !exists {
		return nil, false
	}
	return item.(*v1.Service), true
}

// GetPodLocality retrieves the locality for a pod.
func (c *Controller) GetPodLocality(pod *v1.Pod) string {
	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#late-initialization
	node, exists, err := c.nodes.informer.GetStore().GetByKey(pod.Spec.NodeName)
	if !exists || err != nil {
		log.Warnf("unable to get node %q for pod %q: %v", pod.Spec.NodeName, pod.Name, err)
		return ""
	}

	region, _ := node.(*v1.Node).Labels[NodeRegionLabel]
	zone, _ := node.(*v1.Node).Labels[NodeZoneLabel]
	if region == "" && zone == "" {
		return ""
	}

	return fmt.Sprintf("%v/%v", region, zone)
}

// ManagementPorts implements a service catalog operation
func (c *Controller) ManagementPorts(addr string) model.PortList {
	pod := c.pods.getPodByIP(addr)
	if pod == nil {
		return nil
	}

	managementPorts, err := convertProbesToPorts(&pod.Spec)
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
			p, err := convertProbePort(&container, &container.ReadinessProbe.Handler)
			if err != nil {
				log.Infof("Error while parsing readiness probe port =%v", err)
			}
			probes = append(probes, &model.Probe{
				Port: p,
				Path: container.ReadinessProbe.Handler.HTTPGet.Path,
			})
		}
		if container.LivenessProbe != nil && container.LivenessProbe.Handler.HTTPGet != nil {
			p, err := convertProbePort(&container, &container.LivenessProbe.Handler)
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
func (c *Controller) InstancesByPort(hostname model.Hostname, reqSvcPort int,
	labelsList model.LabelsCollection) ([]*model.ServiceInstance, error) {
	name, namespace, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	// Locate all ports in the actual service

	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()
	if svc == nil {
		return nil, nil
	}

	svcPortEntry, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil, nil
	}

	c.RLock()
	instances := c.externalNameSvcInstanceMap[hostname]
	c.RUnlock()
	if instances != nil {
		return instances, nil
	}

	item, exists, err := c.endpoints.informer.GetStore().GetByKey(KeyFunc(name, namespace))
	if err != nil {
		log.Infof("get endpoint(%s, %s) => error %v", name, namespace, err)
		return nil, nil
	}
	if !exists {
		return nil, nil
	}

	ep := item.(*v1.Endpoints)
	var out []*model.ServiceInstance
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			labels, _ := c.pods.labelsByIP(ea.IP)
			// check that one of the input labels is a subset of the labels
			if !labelsList.HasSubsetOf(labels) {
				continue
			}

			pod := c.pods.getPodByIP(ea.IP)
			az, sa, uid := "", "", ""
			if pod != nil {
				az = c.GetPodLocality(pod)
				sa = kubeToIstioServiceAccount(pod.Spec.ServiceAccountName, pod.GetNamespace())
				uid = fmt.Sprintf("kubernetes://%s.%s", pod.Name, pod.Namespace)
			}

			// identify the port by name. K8S EndpointPort uses the service port name
			for _, port := range ss.Ports {
				if port.Name == "" || // 'name optional if single port is defined'
					svcPortEntry.Name == port.Name {
					out = append(out, &model.ServiceInstance{
						Endpoint: model.NetworkEndpoint{
							Address:     ea.IP,
							Port:        int(port.Port),
							ServicePort: svcPortEntry,
							UID:         uid,
							Network:     c.endpointNetwork(ea.IP),
							Locality:    az,
						},
						Service:        svc,
						Labels:         labels,
						ServiceAccount: sa,
					})
				}
			}
		}
	}

	return out, nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)

	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]
	proxyNamespace := ""

	pod := c.pods.getPodByIP(proxyIP)
	if pod != nil {
		proxyNamespace = pod.Namespace
		// 1. find proxy service by label selector, if not any, there may exist headless service
		// failover to 2
		svcLister := listerv1.NewServiceLister(c.services.informer.GetIndexer())
		if services, err := svcLister.GetPodServices(pod); err == nil && len(services) > 0 {
			for _, svc := range services {
				out = append(out, c.getProxyServiceInstancesByPod(pod, svc, proxy)...)
			}
			return out, nil
		}
	}

	// 2. Headless service
	endpointsForPodInSameNS := make([]*model.ServiceInstance, 0)
	endpointsForPodInDifferentNS := make([]*model.ServiceInstance, 0)
	for _, item := range c.endpoints.informer.GetStore().List() {
		ep := *item.(*v1.Endpoints)
		endpoints := &endpointsForPodInSameNS
		if ep.Namespace != proxyNamespace {
			endpoints = &endpointsForPodInDifferentNS
		}

		*endpoints = append(*endpoints, c.getProxyServiceInstancesByEndpoint(ep, proxy)...)
	}

	// Put the endpointsForPodInSameNS in front of endpointsForPodInDifferentNS so that Pilot will
	// first use endpoints from endpointsForPodInSameNS. This makes sure if there are two endpoints
	// referring to the same IP/port, the one in endpointsForPodInSameNS will be used. (The other one
	// in endpointsForPodInDifferentNS will thus be rejected by Pilot).
	out = append(endpointsForPodInSameNS, endpointsForPodInDifferentNS...)
	if len(out) == 0 {
		if c.Env != nil {
			c.Env.PushContext.Add(model.ProxyStatusNoService, proxy.ID, proxy, "")
			status := c.Env.PushContext
			if status == nil {
				log.Infof("Empty list of services for pod %s %v", proxy.ID, c.Env)
			}
		} else {
			log.Infof("Missing env, empty list of services for pod %s", proxy.ID)
		}
	}
	return out, nil
}

func (c *Controller) getProxyServiceInstancesByEndpoint(endpoints v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := serviceHostname(endpoints.Name, endpoints.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc != nil {
		for _, ss := range endpoints.Subsets {
			for _, port := range ss.Ports {
				svcPort, exists := svc.Ports.Get(port.Name)
				if !exists {
					continue
				}

				// There is only one IP for kube registry
				proxyIP := proxy.IPAddresses[0]

				if hasProxyIP(ss.Addresses, proxyIP) {
					out = append(out, c.getEndpoints(proxyIP, port.Port, svcPort, svc))
				}

				if hasProxyIP(ss.NotReadyAddresses, proxyIP) {
					nrEP := c.getEndpoints(proxyIP, port.Port, svcPort, svc)
					out = append(out, nrEP)
					if c.Env != nil {
						c.Env.PushContext.Add(model.ProxyStatusEndpointNotReady, proxy.ID, proxy, "")
					}
				}
			}
		}
	}

	return out
}

func (c *Controller) getProxyServiceInstancesByPod(pod *v1.Pod, service *v1.Service, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := serviceHostname(service.Name, service.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc == nil {
		return out
	}

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
		// There is only one IP for kube registry
		proxyIP := proxy.IPAddresses[0]

		out = append(out, c.getEndpoints(proxyIP, int32(portNum), svcPort, svc))

	}

	return out
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (model.LabelsCollection, error) {
	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	pod := c.pods.getPodByIP(proxyIP)
	if pod != nil {
		return model.LabelsCollection{pod.Labels}, nil
	}
	return nil, nil
}

func (c *Controller) getEndpoints(ip string, endpointPort int32, svcPort *model.Port, svc *model.Service) *model.ServiceInstance {
	labels, _ := c.pods.labelsByIP(ip)
	pod := c.pods.getPodByIP(ip)
	az, sa := "", ""
	if pod != nil {
		az = c.GetPodLocality(pod)
		sa = kubeToIstioServiceAccount(pod.Spec.ServiceAccountName, pod.GetNamespace())
	}
	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     ip,
			Port:        int(endpointPort),
			ServicePort: svcPort,
			Network:     c.endpointNetwork(ip),
			Locality:    az,
		},
		Service:        svc,
		Labels:         labels,
		ServiceAccount: sa,
	}
}

// GetIstioServiceAccounts returns the Istio service accounts running a serivce
// hostname. Each service account is encoded according to the SPIFFE VSID spec.
// For example, a service account named "bar" in namespace "foo" is encoded as
// "spiffe://cluster.local/ns/foo/sa/bar".
func (c *Controller) GetIstioServiceAccounts(hostname model.Hostname, ports []int) []string {
	saSet := make(map[string]bool)

	// Get the service accounts running the service, if it is deployed on VMs. This is retrieved
	// from the service annotation explicitly set by the operators.
	svc, err := c.GetService(hostname)
	if err != nil {
		// Do not log error here, as the service could exist in another registry
		return nil
	}
	if svc == nil {
		// Do not log error here as the service could exist in another registry
		return nil
	}

	instances := make([]*model.ServiceInstance, 0)
	// Get the service accounts running service within Kubernetes. This is reflected by the pods that
	// the service is deployed on, and the service accounts of the pods.
	for _, port := range ports {
		svcinstances, err := c.InstancesByPort(hostname, port, model.LabelsCollection{})
		if err != nil {
			log.Warnf("InstancesByPort(%s:%d) error: %v", hostname, port, err)
			return nil
		}
		instances = append(instances, svcinstances...)
	}

	for _, si := range instances {
		if si.ServiceAccount != "" {
			saSet[si.ServiceAccount] = true
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
	c.services.handler.Append(func(obj interface{}, event model.Event) error {
		svc, ok := obj.(*v1.Service)
		if !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				log.Errorf("Couldn't get object from tombstone %#v", obj)
				return nil
			}
			svc, ok = tombstone.Obj.(*v1.Service)
			if !ok {
				log.Errorf("Tombstone contained object that is not a service %#v", obj)
				return nil
			}
		}

		log.Infof("Handle service %s in namespace %s", svc.Name, svc.Namespace)

		hostname := svc.Name + "." + svc.Namespace
		ports := map[string]uint32{}
		portsByNum := map[uint32]string{}

		for _, port := range svc.Spec.Ports {
			ports[port.Name] = uint32(port.Port)
			portsByNum[uint32(port.Port)] = port.Name
		}

		svcConv := convertService(*svc, c.domainSuffix)
		instances := externalNameServiceInstances(*svc, svcConv)
		switch event {
		case model.EventDelete:
			c.Lock()
			delete(c.servicesMap, svcConv.Hostname)
			delete(c.externalNameSvcInstanceMap, svcConv.Hostname)
			c.Unlock()
		default:
			c.Lock()
			c.servicesMap[svcConv.Hostname] = svcConv
			if instances == nil {
				delete(c.externalNameSvcInstanceMap, svcConv.Hostname)
			} else {
				c.externalNameSvcInstanceMap[svcConv.Hostname] = instances
			}
			c.Unlock()
		}
		// EDS needs the port mapping.
		c.XDSUpdater.SvcUpdate(c.ClusterID, hostname, ports, portsByNum)

		f(svcConv, event)

		return nil
	})
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	if c.endpoints.handler == nil {
		return nil
	}
	c.endpoints.handler.Append(func(obj interface{}, event model.Event) error {
		ep, ok := obj.(*v1.Endpoints)
		if !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				log.Errorf("Couldn't get object from tombstone %#v", obj)
				return nil
			}
			ep, ok = tombstone.Obj.(*v1.Endpoints)
			if !ok {
				log.Errorf("Tombstone contained object that is not a service %#v", obj)
				return nil
			}
		}

		c.updateEDS(ep, event)

		return nil
	})

	return nil
}

func (c *Controller) updateEDS(ep *v1.Endpoints, event model.Event) {
	hostname := serviceHostname(ep.Name, ep.Namespace, c.domainSuffix)

	endpoints := []*model.IstioEndpoint{}
	if event != model.EventDelete {
		for _, ss := range ep.Subsets {
			for _, ea := range ss.Addresses {
				pod := c.pods.getPodByIP(ea.IP)
				if pod == nil {
					log.Warnf("Endpoint without pod %s %v", ea.IP, ep)
					if c.Env != nil {
						c.Env.PushContext.Add(model.EndpointNoPod, string(hostname), nil, ea.IP)
					}
					// TODO: keep them in a list, and check when pod events happen !
					continue
				}

				labels := map[string]string(convertLabels(pod.ObjectMeta))

				uid := fmt.Sprintf("kubernetes://%s.%s", pod.Name, pod.Namespace)

				// EDS and ServiceEntry use name for service port - ADS will need to
				// map to numbers.
				for _, port := range ss.Ports {
					endpoints = append(endpoints, &model.IstioEndpoint{
						Address:         ea.IP,
						EndpointPort:    uint32(port.Port),
						ServicePortName: port.Name,
						Labels:          labels,
						UID:             uid,
						ServiceAccount:  kubeToIstioServiceAccount(pod.Spec.ServiceAccountName, pod.GetNamespace()),
						Network:         c.endpointNetwork(ea.IP),
					})
				}
			}
		}
	}

	// TODO: Endpoints include the service labels, maybe we can use them ?
	// nodeName is also included, not needed

	log.Infof("Handle EDS endpoint %s in namespace %s -> %v %v", ep.Name, ep.Namespace, ep.Subsets, endpoints)

	c.XDSUpdater.EDSUpdate(c.ClusterID, string(hostname), endpoints)
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

// InitNetworkLookup will read the mesh networks configuration from the environment
// and initialize CIDR rangers for an efficient network lookup when needed
func (c *Controller) InitNetworkLookup(meshNetworks *meshconfig.MeshNetworks) {
	if meshNetworks == nil || len(meshNetworks.Networks) == 0 {
		return
	}

	c.ranger = cidranger.NewPCTrieRanger()

	for n, v := range meshNetworks.Networks {
		for _, ep := range v.Endpoints {
			if ep.GetFromCidr() != "" {
				_, net, err := net.ParseCIDR(ep.GetFromCidr())
				if err != nil {
					log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), n)
					continue
				}
				rangerEntry := namedRangerEntry{
					name:    n,
					network: *net,
				}
				c.ranger.Insert(rangerEntry)
			}
			if ep.GetFromRegistry() != "" && ep.GetFromRegistry() == c.ClusterID {
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
