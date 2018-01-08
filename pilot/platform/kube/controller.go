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
	"reflect"
	"time"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pkg/log"
)

const (
	// NodeRegionLabel is the well-known label for kubernetes node region
	NodeRegionLabel = "failure-domain.beta.kubernetes.io/region"
	// NodeZoneLabel is the well-known label for kubernetes node zone
	NodeZoneLabel = "failure-domain.beta.kubernetes.io/zone"
	// IstioNamespace used by default for Istio cluster-wide installation
	IstioNamespace = "istio-system"
)

// ControllerOptions stores the configurable attributes of a Controller.
type ControllerOptions struct {
	// Namespace the controller watches. If set to meta_v1.NamespaceAll (""), controller watches all namespaces
	WatchedNamespace string
	ResyncPeriod     time.Duration
	DomainSuffix     string

	// Should only be used for pre-primed Kube clients
	IgnoreInformerSync bool
}

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	domainSuffix string

	client             kubernetes.Interface
	ticker             time.Ticker
	controllerPath     string
	handler            *model.ControllerViewHandler
	queue              Queue
	services           cacheHandler
	endpoints          cacheHandler
	nodes              cacheHandler
	pods               *PodCache
	ignoreInformerSync bool
}

type cacheHandler struct {
	informer cache.SharedIndexInformer
	handler  *ChainHandler
}

// NewController creates a new Kubernetes controller
func NewController(client kubernetes.Interface, ticker time.Ticker, options ControllerOptions) *Controller {
	if log.DebugEnabled() {
		log.Debugf("Kubernetes registry controller watching namespace %q", options.WatchedNamespace)
	}

	// Queue requires a time duration for a retry delay after a handler error
	out := &Controller{
		domainSuffix: options.DomainSuffix,
		client:       client,
		ticker:       ticker,
		queue:        NewQueue(1 * time.Second),
	}

	out.services = out.createInformer(&v1.Service{}, options.ResyncPeriod,
		func(opts meta_v1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Services(options.WatchedNamespace).List(opts)
		},
		func(opts meta_v1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Services(options.WatchedNamespace).Watch(opts)
		})

	out.endpoints = out.createInformer(&v1.Endpoints{}, options.ResyncPeriod,
		func(opts meta_v1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Endpoints(options.WatchedNamespace).List(opts)
		},
		func(opts meta_v1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Endpoints(options.WatchedNamespace).Watch(opts)
		})

	out.nodes = out.createInformer(&v1.Node{}, options.ResyncPeriod,
		func(opts meta_v1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Nodes().List(opts)
		},
		func(opts meta_v1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Nodes().Watch(opts)
		})

	out.pods = newPodCache(out.createInformer(&v1.Pod{}, options.ResyncPeriod,
		func(opts meta_v1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(options.WatchedNamespace).List(opts)
		},
		func(opts meta_v1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Pods(options.WatchedNamespace).Watch(opts)
		}))
	out.ignoreInformerSync = options.IgnoreInformerSync

	return out
}

// Handle implements model.Controller interface
func (c *Controller) Handle(path string, handler *model.ControllerViewHandler) error {
	if c.handler != nil {
		err := fmt.Errorf("kubernetes registry already setup to handle mesh view at controller path '%s'",
			c.controllerPath)
		log.Error(err.Error())
		return err
	}
	c.controllerPath = path
	c.handler = handler
	return nil
}

// Run implements model.Controller interface
func (c *Controller) Run(stop <-chan struct{}) {
	log.Infof("Starting Kubernetes registry controller for controller path '%s'", c.controllerPath)
	go c.queue.Run(stop)
	go c.services.informer.Run(stop)
	go c.endpoints.informer.Run(stop)
	go c.pods.informer.Run(stop)
	for {
		// Block until tick
		<-c.ticker.C
		c.doReconcile()
		select {
		case <-stop:
			log.Infof("Stopping Kubernetes registry controller for controller path '%s'", c.controllerPath)
			return
		default:
		}
	}
}

// notify is the first handler in the handler chain.
// Returning an error causes repeated execution of the entire chain.
func (c *Controller) notify(obj interface{}, event model.Event) error {
	if !c.hasSynced() {
		return errors.New("waiting till full synchronization")
	}
	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if log.DebugEnabled() {
		if err != nil {
			log.Debugf("Error retrieving key: %v", err)
		} else {
			log.Debugf("Event %s: key %#v", event, k)
		}
	}
	return nil
}

func (c *Controller) createInformer(
	o runtime.Object,
	resyncPeriod time.Duration,
	lf cache.ListFunc,
	wf cache.WatchFunc) cacheHandler {
	handler := &ChainHandler{funcs: []Handler{c.notify}}

	// TODO: finer-grained index (perf)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{ListFunc: lf, WatchFunc: wf}, o,
		resyncPeriod, cache.Indexers{})

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventAdd})
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					c.queue.Push(Task{handler: handler.Apply, obj: cur, event: model.EventUpdate})
				}
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler.Apply, obj: obj, event: model.EventDelete})
			},
		})

	return cacheHandler{informer: informer, handler: handler}
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) hasSynced() bool {
	if c.ignoreInformerSync {
		return true
	}
	if !c.services.informer.HasSynced() ||
		!c.endpoints.informer.HasSynced() ||
		!c.pods.informer.HasSynced() {
		return false
	}
	return true
}

func (c *Controller) doReconcile() {
	if !c.hasSynced() {
		log.Infof("Kubernetes registry controller for path '%s' waiting for initialization", c.controllerPath)
		return
	}
	controllerView := model.ControllerView{
		Path:             c.controllerPath,
		Services:         []*model.Service{},
		ServiceInstances: []*model.ServiceInstance{},
	}
	list := c.services.informer.GetStore().List()
	keySvcMap := map[string]*model.Service{}
	type PortMap map[int]*model.Port
	svcPortMap := map[string]PortMap{}
	for _, item := range list {
		kubeSvc := item.(*v1.Service)
		if svc := convertService(*kubeSvc, c.domainSuffix); svc != nil {
			controllerView.Services = append(controllerView.Services, svc)
			svcKey := KeyFunc(kubeSvc.Name, kubeSvc.Namespace)
			keySvcMap[svcKey] = svc
			portMap, found := svcPortMap[svcKey]
			if !found {
				portMap = PortMap{}
				svcPortMap[svcKey] = portMap
			}
			for _, port := range svc.Ports {
				portMap[port.Port] = port
			}
		}
	}
	for _, kubeEp := range c.endpoints.informer.GetStore().List() {
		ep := *kubeEp.(*v1.Endpoints)
		svcKey := KeyFunc(ep.Name, ep.Namespace)
		svc, found := keySvcMap[svcKey]
		if found {
			for _, ss := range ep.Subsets {
				for _, ea := range ss.Addresses {
					labels, _ := c.pods.labelsByIP(ea.IP)
					pod, exists := c.pods.getPodByIP(ea.IP)
					az, sa := "", ""
					if exists {
						az, _ = c.getPodAZ(pod)
						sa = kubeToIstioServiceAccount(pod.Spec.ServiceAccountName, pod.GetNamespace(), c.domainSuffix)
					}
					// identify the port by name
					portMap, portsFound := svcPortMap[svcKey]
					if !portsFound {
						// Not a typical scenario for a service not to have ports
						// Nevertheless skip and wait for next reconcile.

						continue
					}
					for _, port := range ss.Ports {
						svcPort, svcPortFound := portMap[int(port.Port)]
						if !svcPortFound {
							// Rare scenario where endpoints updated
							// out of order with respect to service
							// Wait for next reconcile.
							continue
						}
						controllerView.ServiceInstances =
							append(controllerView.ServiceInstances, &model.ServiceInstance{
								Endpoint: model.NetworkEndpoint{
									Address:     ea.IP,
									Port:        int(port.Port),
									ServicePort: svcPort,
								},
								Service:          svc,
								Labels:           labels,
								AvailabilityZone: az,
								ServiceAccount:   sa,
							})
					}
				}
			}
		}
	}
	(*c.handler).Reconcile(&controllerView)
}

// getPodAZ retrieves the AZ for a pod.
func (c *Controller) getPodAZ(pod *v1.Pod) (string, bool) {
	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#late-initialization
	node, exists, err := c.nodes.informer.GetStore().GetByKey(pod.Spec.NodeName)
	if !exists || err != nil {
		return "", false
	}
	region, exists := node.(*v1.Node).Labels[NodeRegionLabel]
	if !exists {
		return "", false
	}
	zone, exists := node.(*v1.Node).Labels[NodeZoneLabel]
	if !exists {
		return "", false
	}

	return fmt.Sprintf("%v/%v", region, zone), true
}
