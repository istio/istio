// Copyright 2017 Google Inc.
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
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"github.com/golang/glog"

	"istio.io/manager/model"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	client *Client
	queue  Queue

	kinds     map[string]cacheHandler
	services  cacheHandler
	endpoints cacheHandler
	pods      cacheHandler
}

type cacheHandler struct {
	informer cache.SharedIndexInformer
	handler  *chainHandler
}

// NewController creates a new Kubernetes controller
func NewController(
	client *Client,
	namespace string,
	resyncPeriod time.Duration,
) *Controller {
	// Queue requires a time duration for a retry delay after a handler error
	out := &Controller{
		client: client,
		queue:  NewQueue(1 * time.Second),
		kinds:  make(map[string]cacheHandler),
	}

	out.services = out.createInformer(&v1.Service{}, resyncPeriod,
		func(opts v1.ListOptions) (runtime.Object, error) {
			return client.client.Services(namespace).List(opts)
		},
		func(opts v1.ListOptions) (watch.Interface, error) {
			return client.client.Services(namespace).Watch(opts)
		})

	out.endpoints = out.createInformer(&v1.Endpoints{}, resyncPeriod,
		func(opts v1.ListOptions) (runtime.Object, error) {
			return client.client.Endpoints(namespace).List(opts)
		},
		func(opts v1.ListOptions) (watch.Interface, error) {
			return client.client.Endpoints(namespace).Watch(opts)
		})

	out.pods = out.createInformer(&v1.Pod{}, resyncPeriod,
		func(opts v1.ListOptions) (runtime.Object, error) {
			return client.client.Pods(namespace).List(opts)
		},
		func(opts v1.ListOptions) (watch.Interface, error) {
			return client.client.Pods(namespace).Watch(opts)
		})

	// add stores for TPR kinds
	for kind := range client.mapping {
		out.kinds[kind] = out.createInformer(&Config{}, resyncPeriod,
			func(opts v1.ListOptions) (result runtime.Object, err error) {
				result = &ConfigList{}
				err = client.dyn.Get().
					Namespace(namespace).
					Resource(kind+"s").
					VersionedParams(&opts, api.ParameterCodec).
					Do().
					Into(result)
				return
			},
			func(opts v1.ListOptions) (watch.Interface, error) {
				return client.dyn.Get().
					Prefix("watch").
					Namespace(namespace).
					Resource(kind+"s").
					VersionedParams(&opts, api.ParameterCodec).
					Watch()
			})
	}

	return out
}

// notify is the first handler in the handler chain.
// Returning an error causes repeated execution of the entire chain.
func (c *Controller) notify(obj interface{}, event model.Event) error {
	if !c.HasSynced() {
		return errors.New("Waiting till full synchronization")
	}
	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		glog.V(2).Infof("Error retrieving key: %v", err)
	} else {
		glog.V(2).Infof("Event %s: key %#v", event, k)
	}
	return nil
}

func (c *Controller) createInformer(
	o runtime.Object,
	resyncPeriod time.Duration,
	lf cache.ListFunc,
	wf cache.WatchFunc) cacheHandler {
	handler := &chainHandler{funcs: []Handler{c.notify}}

	// TODO: finer-grained index (perf)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{ListFunc: lf, WatchFunc: wf}, o,
		resyncPeriod, cache.Indexers{})

	err := informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler.apply, obj: obj, event: model.EventAdd})
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					c.queue.Push(Task{handler: handler.apply, obj: cur, event: model.EventUpdate})
				}
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler.apply, obj: obj, event: model.EventDelete})
			},
		})
	if err != nil {
		glog.Warning(err)
	}

	return cacheHandler{informer: informer, handler: handler}
}

// AppendConfigHandler adds a notification handler.
func (c *Controller) AppendConfigHandler(kind string, f func(*model.Config, model.Event)) error {
	ch, ok := c.kinds[kind]
	if !ok {
		return fmt.Errorf("Cannot locate kind %q", kind)
	}
	ch.handler.append(func(obj interface{}, ev model.Event) error {
		cfg, err := kubeToModel(kind, c.client.mapping[kind], obj.(*Config))
		if err == nil {
			f(cfg, ev)
		} else {
			// Do not trigger re-application of handlers
			glog.Warningf("Cannot convert kind %s to a config object", kind)
		}
		return nil
	})
	return nil
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	if !c.services.informer.HasSynced() ||
		!c.endpoints.informer.HasSynced() ||
		!c.pods.informer.HasSynced() {
		return false
	}
	for kind, ctl := range c.kinds {
		if !ctl.informer.HasSynced() {
			glog.V(2).Infof("Controller %q is syncing...", kind)
			return false
		}
	}
	return true
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop chan struct{}) {
	go c.queue.Run(stop)
	go c.services.informer.Run(stop)
	go c.endpoints.informer.Run(stop)
	go c.pods.informer.Run(stop)
	for _, ctl := range c.kinds {
		go ctl.informer.Run(stop)
	}
	<-stop
	glog.V(2).Info("Controller terminated")
}

// keyFunc is the internal key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func keyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

// Get implements a registry operation
func (c *Controller) Get(key model.ConfigKey) (*model.Config, bool) {
	if err := c.client.mapping.ValidateKey(&key); err != nil {
		glog.Warning(err)
		return nil, false
	}

	store := c.kinds[key.Kind].informer.GetStore()
	data, exists, err := store.GetByKey(key.Namespace + "/" + key.Name)
	if !exists {
		return nil, false
	}
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	out, err := kubeToModel(key.Kind, c.client.mapping[key.Kind], data.(*Config))
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	return out, true
}

// Put implements a registry operation
func (c *Controller) Put(obj *model.Config) error {
	return c.client.Put(obj)
}

// Delete implements a registry operation
func (c *Controller) Delete(key model.ConfigKey) error {
	return c.client.Delete(key)
}

// List implements a registry operation
func (c *Controller) List(kind string, ns string) ([]*model.Config, error) {
	if _, ok := c.kinds[kind]; !ok {
		return nil, fmt.Errorf("Missing kind %q", kind)
	}

	// TODO: use indexed cache
	var out []*model.Config
	var errs error
	for _, data := range c.kinds[kind].informer.GetStore().List() {
		config := data.(*Config)
		if ns == "" || config.Metadata.Namespace == ns {
			elt, err := kubeToModel(kind, c.client.mapping[kind], data.(*Config))
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				out = append(out, elt)
			}
		}
	}
	return out, errs
}

// Services implements a service catalog operation
func (c *Controller) Services() []*model.Service {
	var out []*model.Service
	for _, item := range c.services.informer.GetStore().List() {
		out = append(out, convertService(*item.(*v1.Service)))
	}
	return out
}

// GetService implements a service catalog operation
func (c *Controller) GetService(name string, namespace string) (*model.Service, bool) {
	item, exists, err := c.services.informer.GetStore().GetByKey(keyFunc(name, namespace))
	if err != nil {
		glog.V(2).Infof("GetService(%s, %s) => error %v", name, namespace, err)
		return nil, false
	}
	if !exists {
		return nil, false
	}
	return convertService(*item.(*v1.Service)), true
}

// Instances implements a service catalog operation
func (c *Controller) Instances(query *model.Service) []*model.ServiceInstance {
	// Get actual service by name
	svc, exists := c.GetService(query.Name, query.Namespace)
	if !exists {
		return nil
	}

	// Locate all ports in the actual service
	ports := make(map[string]*model.Port)
	if len(query.Ports) == 0 {
		for _, port := range svc.Ports {
			ports[port.Name] = port
		}
	} else {
		for _, port := range query.Ports {
			if svcPort, exists := svc.GetPort(port.Name); exists {
				ports[port.Name] = svcPort
			}
		}
	}

	for _, item := range c.endpoints.informer.GetStore().List() {
		ep := *item.(*v1.Endpoints)
		if ep.Name == svc.Name && ep.Namespace == svc.Namespace {
			var out []*model.ServiceInstance
			for _, ss := range ep.Subsets {
				for _, ea := range ss.Addresses {
					for _, port := range ss.Ports {
						if svcPort, exists := ports[port.Name]; exists {
							out = append(out, &model.ServiceInstance{
								Endpoint: model.Endpoint{
									Address:     ea.IP,
									Port:        int(port.Port),
									ServicePort: svcPort,
								},
								Service: svc,
								Tag:     nil,
							})
						}
					}
				}
			}
			return out
		}
	}
	return nil
}

// HostInstances implements a service catalog operation
func (c *Controller) HostInstances(addrs map[string]bool) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	for _, item := range c.endpoints.informer.GetStore().List() {
		ep := *item.(*v1.Endpoints)
		for _, ss := range ep.Subsets {
			for _, ea := range ss.Addresses {
				if addrs[ea.IP] {
					for _, port := range ss.Ports {
						svc, exists := c.GetService(ep.Name, ep.Namespace)
						if !exists {
							continue
						}
						svcPort, exists := svc.GetPort(port.Name)
						if !exists {
							continue
						}
						out = append(out, &model.ServiceInstance{
							Service: svc,
							Tag:     nil,
							Endpoint: model.Endpoint{
								Address:     ea.IP,
								Port:        int(port.Port),
								ServicePort: svcPort,
							},
						})
					}
				}
			}
		}
	}
	return out
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.services.handler.append(func(obj interface{}, event model.Event) error {
		f(convertService(*obj.(*v1.Service)), event)
		return nil
	})
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.Service, model.Event)) error {
	c.endpoints.handler.append(func(obj interface{}, event model.Event) error {
		ep := *obj.(*v1.Endpoints)
		f(&model.Service{
			Name:      ep.Name,
			Namespace: ep.Namespace,
		}, event)
		return nil
	})
	return nil
}

func convertService(svc v1.Service) *model.Service {
	ports := make([]*model.Port, 0)
	addrs := make(map[string]bool)
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		addrs[svc.Spec.ClusterIP] = true
	}

	for _, port := range svc.Spec.Ports {
		ports = append(ports, &model.Port{
			Name:     port.Name,
			Port:     int(port.Port),
			Protocol: convertProtocol(port.Name, port.Protocol),
		})
	}

	return &model.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Ports:     ports,
		Addresses: addrs,
		// TODO: empty set of service tags for now
		Tags: nil,
	}
}

func convertProtocol(name string, proto v1.Protocol) model.Protocol {
	out := model.ProtocolTCP
	switch proto {
	case v1.ProtocolUDP:
		out = model.ProtocolUDP
	case v1.ProtocolTCP:
		prefix := name
		i := strings.Index(name, "-")
		if i >= 0 {
			prefix = name[:i]
		}
		switch prefix {
		case "grpc":
			out = model.ProtocolGRPC
		case "http":
			out = model.ProtocolHTTP
		case "http2":
			out = model.ProtocolHTTP2
		case "https":
			out = model.ProtocolHTTPS
		}
	}
	return out
}
