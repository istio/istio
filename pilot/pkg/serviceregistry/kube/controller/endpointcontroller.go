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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface
// that both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	Run(stopCh <-chan struct{})
	InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
		labelsList labels.Collection) ([]*model.ServiceInstance, error)
	GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance
	buildIstioEndpoints(ep interface{}, host host.Name) []*model.IstioEndpoint
	getServiceInfo(ep interface{}) (host.Name, string, string)
}

// kubeEndpoints abstracts the common behavior across endpoint and endpoint slices.
type kubeEndpoints struct {
	c          *Controller
	informer   cache.SharedIndexInformer
	needResync map[string]sets.Set
}

func (e *kubeEndpoints) HasSynced() bool {
	return e.informer.HasSynced()
}

func (e *kubeEndpoints) Run(stopCh <-chan struct{}) {
	e.informer.Run(stopCh)
	go e.reSyncEndpoints(stopCh)
}

// processEndpointEvent triggers the config update.
func processEndpointEvent(c *Controller, epc kubeEndpointsController, name string, namespace string, event model.Event, ep interface{}) error {
	log.Debugf("Handle event %s for endpoint %s in namespace %s", event, name, namespace)
	if features.EnableHeadlessService {
		if svc, _ := c.serviceLister.Services(namespace).Get(name); svc != nil {
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				c.xdsUpdater.ConfigUpdate(&model.PushRequest{
					Full: true,
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigsUpdated: map[model.ConfigKey]struct{}{{
						Kind:      gvk.ServiceEntry,
						Name:      svc.Name,
						Namespace: svc.Namespace,
					}: {}},
					Reason: []model.TriggerReason{model.EndpointUpdate},
				})
				return nil
			}
		}
	}

	updateEDS(c, epc, ep, event)

	return nil
}

func updateEDS(c *Controller, epc kubeEndpointsController, ep interface{}, event model.Event) {
	host, svcName, ns := epc.getServiceInfo(ep)
	c.RLock()
	svc := c.servicesMap[host]
	c.RUnlock()

	if svc == nil {
		log.Infof("Handle EDS endpoint: skip updating, service %s/%s has mot been populated", svcName, ns)
		return
	}

	log.Debugf("Handle EDS endpoint %s in namespace %s", svcName, ns)
	var endpoints []*model.IstioEndpoint
	if event != model.EventDelete {
		endpoints = epc.buildIstioEndpoints(ep, host)
	}
	fep := c.collectAllForeignEndpoints(svc)
	_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(host), ns, append(endpoints, fep...))
	// fire instance handles for k8s endpoints only
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

// reSyncEndpoints syncs endpoints that were not processed because of unavailibility of pod
// when endpoint event came. Try to reprocess it to see if pod is available or endpoint it self
// might have been deleted.
func (e *kubeEndpoints) reSyncEndpoints(stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second) // TODO: Make this resync period configurable.
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for ip := range e.needResync {
				if endpoints, f := e.needResync[ip]; f {
					for ep := range endpoints {
						delete(e.needResync[ip], ep)
						item, exists, err := e.informer.GetStore().GetByKey(ep)
						if err != nil {
							log.Errorf("error rerieving endpoint %s: %v", ep, err)
							continue
						}
						if !exists {
							log.Debugf("endpoint %v does not exist. So ignoring it", ep)
							// must be stale, do nothing
							continue
						}
						// Trigger event on right controller.
						switch epc := e.c.endpoints.(type) {
						case *endpointsController:
							e.c.queue.Push(func() error {
								return epc.onEvent(item, model.EventUpdate)
							})
						case *endpointSliceController:
							e.c.queue.Push(func() error {
								return epc.onEvent(item, model.EventUpdate)
							})
						}
					}
					delete(e.needResync, ip)
				}
			}
			syncQueueSize.Record(float64(len(e.needResync)))
		case <-stopCh:
			return
		}
	}
}
