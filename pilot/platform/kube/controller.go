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
	"strconv"
	"time"

	"github.com/golang/protobuf/proto"

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

	pods *PodCache
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

	out.pods = newPodCache(out.createInformer(&v1.Pod{}, resyncPeriod,
		func(opts v1.ListOptions) (runtime.Object, error) {
			return client.client.Pods(namespace).List(opts)
		},
		func(opts v1.ListOptions) (watch.Interface, error) {
			return client.client.Pods(namespace).Watch(opts)
		}))

	// add stores for TPR kinds
	for _, kind := range []string{IstioKind} {
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
func (c *Controller) AppendConfigHandler(k string, f func(model.Key, proto.Message, model.Event)) error {
	c.kinds[IstioKind].handler.append(func(obj interface{}, ev model.Event) error {
		config, ok := obj.(*Config)
		if ok {
			name, namespace, kind, data, err := c.client.convertConfig(config)
			if kind == k {
				if err == nil {
					f(model.Key{
						Name:      name,
						Namespace: namespace,
						Kind:      kind,
					}, data, ev)
				} else {
					// Do not trigger re-application of handlers
					glog.Warningf("Cannot convert kind %s to a config object", kind)
				}
			}
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
func (c *Controller) Get(key model.Key) (proto.Message, bool) {
	if err := c.client.mapping.ValidateKey(&key); err != nil {
		glog.Warning(err)
		return nil, false
	}

	store := c.kinds[IstioKind].informer.GetStore()
	data, exists, err := store.GetByKey(key.Namespace + "/" + configKey(&key))
	if !exists {
		return nil, false
	}
	if err != nil {
		glog.Warning(err)
		return nil, false
	}

	config, ok := data.(*Config)
	if !ok {
		glog.Warning("Cannot convert to config from store")
		return nil, false
	}

	kind := c.client.mapping[key.Kind]
	out, err := kind.FromJSONMap(config.Spec)
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	return out, true
}

// Put implements a registry operation
func (c *Controller) Put(k model.Key, v proto.Message) error {
	return c.client.Put(k, v)
}

// Delete implements a registry operation
func (c *Controller) Delete(key model.Key) error {
	return c.client.Delete(key)
}

// List implements a registry operation
func (c *Controller) List(k, namespace string) (map[model.Key]proto.Message, error) {
	if _, ok := c.client.mapping[k]; !ok {
		return nil, fmt.Errorf("Missing kind %q", k)
	}

	var errs error
	out := make(map[model.Key]proto.Message, 0)
	for _, data := range c.kinds[IstioKind].informer.GetStore().List() {
		item, ok := data.(*Config)
		if ok && (namespace == "" || item.Metadata.Namespace == namespace) {
			name, ns, kind, data, err := c.client.convertConfig(item)
			if kind == k {
				if err != nil {
					errs = multierror.Append(errs, err)
				} else {
					out[model.Key{
						Name:      name,
						Namespace: ns,
						Kind:      kind,
					}] = data
				}
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
func (c *Controller) GetService(hostname string) (*model.Service, bool) {
	name, namespace, err := parseHostname(hostname)
	if err != nil {
		glog.V(2).Infof("GetService(%s) => error %v", hostname, err)
		return nil, false
	}
	item, exists := c.serviceByKey(name, namespace)
	if !exists {
		return nil, false
	}

	return convertService(*item), true
}

// serviceByKey retrieves a service by name and namespace
func (c *Controller) serviceByKey(name, namespace string) (*v1.Service, bool) {
	item, exists, err := c.services.informer.GetStore().GetByKey(keyFunc(name, namespace))
	if err != nil {
		glog.V(2).Infof("serviceByKey(%s, %s) => error %v", name, namespace, err)
		return nil, false
	}
	if !exists {
		return nil, false
	}
	return item.(*v1.Service), true
}

// Instances implements a service catalog operation
func (c *Controller) Instances(hostname string, ports []string, tags model.TagList) []*model.ServiceInstance {
	// Get actual service by name
	name, namespace, err := parseHostname(hostname)
	if err != nil {
		glog.V(2).Infof("parseHostname(%s) => error %v", hostname, err)
		return nil
	}

	item, exists := c.serviceByKey(name, namespace)
	if !exists {
		return nil
	}

	// Locate all ports in the actual service
	svc := convertService(*item)
	svcPorts := make(map[string]*model.Port)
	for _, port := range ports {
		if svcPort, exists := svc.Ports.Get(port); exists {
			svcPorts[port] = svcPort
		}
	}

	switch item.Spec.Type {
	case v1.ServiceTypeClusterIP, v1.ServiceTypeNodePort:
	case v1.ServiceTypeLoadBalancer:
		// TODO: load balancer service will get traffic from external IPs
	case v1.ServiceTypeExternalName:
		// resolve to external name service, and update name and namespace
		target, exists := c.GetService(item.Spec.ExternalName)
		if !exists {
			glog.V(2).Infof("Missing target service %q for %q", item.Spec.ExternalName, hostname)
			return nil
		}
		name, namespace, err = parseHostname(target.Hostname)
		if err != nil {
			return nil
		}

		// rewrite service ports to use target service port names, which are just numbers
		targetPorts := make(map[string]*model.Port)
		for _, port := range svcPorts {
			targetPorts[strconv.Itoa(port.Port)] = port
		}
		svcPorts = targetPorts
	default:
		glog.Warningf("Unexpected service type %q", item.Spec.Type)
		return nil
	}

	// TODO: single port service missing name
	for _, item := range c.endpoints.informer.GetStore().List() {
		ep := *item.(*v1.Endpoints)
		if ep.Name == name && ep.Namespace == namespace {
			var out []*model.ServiceInstance
			for _, ss := range ep.Subsets {
				for _, ea := range ss.Addresses {
					tag, _ := c.pods.tagByIP(ea.IP)

					// check that one of the input tags is a subset of this tag
					if !tags.HasSubsetOf(tag) {
						continue
					}

					// identify the port by name
					for _, port := range ss.Ports {
						if svcPort, exists := svcPorts[port.Name]; exists {
							out = append(out, &model.ServiceInstance{
								Endpoint: model.NetworkEndpoint{
									Address:     ea.IP,
									Port:        int(port.Port),
									ServicePort: svcPort,
								},
								Service: svc,
								Tag:     tag,
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
						item, exists := c.serviceByKey(ep.Name, ep.Namespace)
						if !exists {
							continue
						}
						svc := convertService(*item)
						svcPort, exists := svc.Ports.Get(port.Name)
						if !exists {
							continue
						}
						tag, _ := c.pods.tagByIP(ea.IP)
						out = append(out, &model.ServiceInstance{
							Endpoint: model.NetworkEndpoint{
								Address:     ea.IP,
								Port:        int(port.Port),
								ServicePort: svcPort,
							},
							Service: svc,
							Tag:     tag,
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
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.endpoints.handler.append(func(obj interface{}, event model.Event) error {
		ep := *obj.(*v1.Endpoints)
		item, exists := c.serviceByKey(ep.Name, ep.Namespace)
		var svc *model.Service
		if exists {
			svc = convertService(*item)
		}
		// TODO: we're passing an incomplete instance to the handler since endpoints is an aggregate
		// structure
		f(&model.ServiceInstance{Service: svc}, event)
		return nil
	})
	return nil
}

// PodCache is an eventually consistent pod cache
type PodCache struct {
	cacheHandler

	// keys maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP
	keys map[string]string
}

func newPodCache(ch cacheHandler) *PodCache {
	out := &PodCache{
		cacheHandler: ch,
		keys:         make(map[string]string),
	}

	ch.handler.append(func(obj interface{}, ev model.Event) error {
		pod := *obj.(*v1.Pod)
		ip := pod.Status.PodIP
		if len(ip) > 0 {
			switch ev {
			case model.EventAdd, model.EventUpdate:
				out.keys[ip] = keyFunc(pod.Name, pod.Namespace)
			case model.EventDelete:
				delete(out.keys, ip)
			}
		}
		return nil
	})
	return out
}

// tagByIP returns pod tags or nil if pod not found or an error occurred
func (pc *PodCache) tagByIP(addr string) (model.Tag, bool) {
	key, exists := pc.keys[addr]
	if !exists {
		return nil, false
	}
	item, exists, err := pc.informer.GetStore().GetByKey(key)
	if !exists || err != nil {
		return nil, false
	}
	return convertTags(item.(*v1.Pod).ObjectMeta), true
}
