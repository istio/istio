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

package tpr

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
)

// Controller is a collection of synchronized resource watchers
// Caches are thread-safe
type Controller struct {
	mesh         *proxyconfig.ProxyMeshConfig
	domainSuffix string

	client    *Client
	queue     kube.Queue
	kinds     map[string]cacheHandler
	ingresses cacheHandler
}

type cacheHandler struct {
	informer cache.SharedIndexInformer
	handler  *kube.ChainHandler
}

// NewController creates a new Kubernetes controller
func NewController(client *Client, mesh *proxyconfig.ProxyMeshConfig, options kube.ControllerOptions) *Controller {
	// Queue requires a time duration for a retry delay after a handler error
	out := &Controller{
		mesh:         mesh,
		domainSuffix: options.DomainSuffix,
		client:       client,
		queue:        kube.NewQueue(1 * time.Second),
		kinds:        make(map[string]cacheHandler),
	}

	if mesh.IngressControllerMode != proxyconfig.ProxyMeshConfig_OFF {
		out.ingresses = out.createInformer(&v1beta1.Ingress{}, options.ResyncPeriod,
			func(opts meta_v1.ListOptions) (runtime.Object, error) {
				return client.client.ExtensionsV1beta1().Ingresses(options.Namespace).List(opts)
			},
			func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.client.ExtensionsV1beta1().Ingresses(options.Namespace).Watch(opts)
			})
	}

	// add stores for TPR kinds
	for _, kind := range []string{IstioKind} {
		out.kinds[kind] = out.createInformer(&Config{}, options.ResyncPeriod,
			func(opts meta_v1.ListOptions) (result runtime.Object, err error) {
				result = &ConfigList{}
				err = client.dynamic.Get().
					Namespace(options.Namespace).
					Resource(kind+"s").
					VersionedParams(&opts, api.ParameterCodec).
					Do().
					Into(result)
				return
			},
			func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.dynamic.Get().
					Prefix("watch").
					Namespace(options.Namespace).
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
	handler := &kube.ChainHandler{}
	handler.Append(c.notify)

	// TODO: finer-grained index (perf)
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{ListFunc: lf, WatchFunc: wf}, o,
		resyncPeriod, cache.Indexers{})

	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				c.queue.Push(kube.NewTask(handler.Apply, obj, model.EventAdd))
			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					c.queue.Push(kube.NewTask(handler.Apply, cur, model.EventUpdate))
				}
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Push(kube.NewTask(handler.Apply, obj, model.EventDelete))
			},
		})

	return cacheHandler{informer: informer, handler: handler}
}

// RegisterEventHandler adds a notification handler.
func (c *Controller) RegisterEventHandler(typ string, f func(model.Config, model.Event)) {
	switch typ {
	case model.IngressRule:
		c.appendIngressConfigHandler(typ, f)
	default:
		c.appendTPRConfigHandler(typ, f)
	}
}

func (c *Controller) appendTPRConfigHandler(typ string, f func(model.Config, model.Event)) {
	c.kinds[IstioKind].handler.Append(func(obj interface{}, ev model.Event) error {
		tpr, ok := obj.(*Config)
		if ok {
			config, err := c.client.convertConfig(tpr)
			if config.Type == typ {
				if err == nil {
					f(config, ev)
				} else {
					// Do not trigger re-application of handlers
					glog.Warningf("Cannot convert kind %s to a config object", typ)
				}
			}
		}
		return nil
	})
}

func (c *Controller) appendIngressConfigHandler(k string, f func(model.Config, model.Event)) {
	if c.mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
		glog.Warning("cannot append ingress config handler: ingress resources synchronization is off")
		return
	}

	c.ingresses.handler.Append(func(obj interface{}, ev model.Event) error {
		ingress := obj.(*v1beta1.Ingress)
		if !c.shouldProcessIngress(ingress) {
			return nil
		}

		// Convert the ingress into a map[Key]Message, and invoke handler for each
		// TODO: This works well for Add and Delete events, but no so for Update:
		// A updated ingress may also trigger an Add or Delete for one of its constituent sub-rules.
		messages := convertIngress(*ingress, c.domainSuffix)
		for key, message := range messages {
			f(model.Config{
				Type:    model.IngressRule,
				Key:     key,
				Content: message,
			}, ev)
		}

		return nil
	})
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	if c.mesh.IngressControllerMode != proxyconfig.ProxyMeshConfig_OFF && !c.ingresses.informer.HasSynced() {
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
func (c *Controller) Run(stop <-chan struct{}) {
	go c.queue.Run(stop)

	if c.mesh.IngressControllerMode != proxyconfig.ProxyMeshConfig_OFF {
		go c.ingresses.informer.Run(stop)
	}

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

// ConfigDescriptor ...
func (c *Controller) ConfigDescriptor() model.ConfigDescriptor {
	return c.client.ConfigDescriptor()
}

// Get implements a registry operation
func (c *Controller) Get(typ, key string) (proto.Message, bool, string) {
	switch typ {
	case model.IngressRule:
		return c.getIngress(key)
	default:
		return c.getTPR(typ, key)
	}
}

func (c *Controller) getTPR(typ, key string) (proto.Message, bool, string) {
	schema, exists := c.client.ConfigDescriptor().GetByType(typ)
	if !exists {
		return nil, false, ""
	}

	store := c.kinds[IstioKind].informer.GetStore()
	data, exists, err := store.GetByKey(keyFunc(configKey(typ, key), c.client.namespace))
	if !exists {
		return nil, false, ""
	}
	if err != nil {
		glog.Warning(err)
		return nil, false, ""
	}

	config, ok := data.(*Config)
	if !ok {
		glog.Warning("Cannot convert to config from store")
		return nil, false, ""
	}

	out, err := schema.FromJSONMap(config.Spec)
	if err != nil {
		glog.Warning(err)
		return nil, false, ""
	}
	return out, true, config.Metadata.ResourceVersion
}

func (c *Controller) getIngress(key string) (proto.Message, bool, string) {
	if c.mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
		glog.Warningf("Cannot get ingress resource for key %v: ingress resources synchronization is off", key)
		return nil, false, ""
	}

	ingressName, ingressNamespace, _, _, err := decodeIngressRuleName(key)
	if err != nil {
		glog.V(2).Infof("getIngress(%s) => error %v", key, err)
		return nil, false, ""
	}
	storeKey := keyFunc(ingressName, ingressNamespace)

	obj, exists, err := c.ingresses.informer.GetStore().GetByKey(storeKey)
	if err != nil {
		glog.V(2).Infof("getIngress(%s) => error %v", key, err)
		return nil, false, ""
	}
	if !exists {
		return nil, false, ""
	}

	ingress := obj.(*v1beta1.Ingress)
	if !c.shouldProcessIngress(ingress) {
		return nil, false, ""
	}

	messages := convertIngress(*ingress, c.domainSuffix)
	message, exists := messages[key]
	return message, exists, ingress.GetResourceVersion()
}

// Post implements a registry operation
func (c *Controller) Post(val proto.Message) (string, error) {
	return c.client.Post(val)
}

// Put implements a registry operation
func (c *Controller) Put(val proto.Message, revision string) (string, error) {
	return c.client.Put(val, revision)
}

// Delete implements a registry operation
func (c *Controller) Delete(typ, key string) error {
	return c.client.Delete(typ, key)
}

// List implements a registry operation
func (c *Controller) List(typ string) ([]model.Config, error) {
	switch typ {
	case model.IngressRule:
		return c.listIngresses()
	default:
		return c.listTPRs(typ)
	}
}

func (c *Controller) listTPRs(typ string) ([]model.Config, error) {
	if _, ok := c.client.ConfigDescriptor().GetByType(typ); !ok {
		return nil, fmt.Errorf("missing type %q", typ)
	}

	var errs error
	out := make([]model.Config, 0)
	for _, data := range c.kinds[IstioKind].informer.GetStore().List() {
		item, ok := data.(*Config)
		if ok {
			config, err := c.client.convertConfig(item)
			if config.Type == typ {
				if err != nil {
					errs = multierror.Append(errs, err)
				} else {
					out = append(out, config)
				}
			}
		}
	}
	return out, errs
}

func (c *Controller) listIngresses() ([]model.Config, error) {
	out := make([]model.Config, 0)

	if c.mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
		glog.Warningf("Cannot list ingress resources: ingress resources synchronization is off")
		return out, nil
	}

	for _, obj := range c.ingresses.informer.GetStore().List() {
		ingress := obj.(*v1beta1.Ingress)
		if c.shouldProcessIngress(ingress) {
			ingressRules := convertIngress(*ingress, c.domainSuffix)
			for key, message := range ingressRules {
				out = append(out, model.Config{
					Type:     model.IngressRule,
					Key:      key,
					Revision: ingress.GetResourceVersion(),
					Content:  message,
				})
			}
		}
	}

	return out, nil
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the Controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func (c *Controller) shouldProcessIngress(ingress *v1beta1.Ingress) bool {
	class, exists := "", false
	if ingress.Annotations != nil {
		class, exists = ingress.Annotations[kube.IngressClassAnnotation]
	}

	switch c.mesh.IngressControllerMode {
	case proxyconfig.ProxyMeshConfig_OFF:
		return false
	case proxyconfig.ProxyMeshConfig_STRICT:
		return exists && class == c.mesh.IngressClass
	case proxyconfig.ProxyMeshConfig_DEFAULT:
		return !exists || class == c.mesh.IngressClass
	default:
		glog.Warningf("Invalid ingress synchronization mode: %v", c.mesh.IngressControllerMode)
		return false
	}
}
