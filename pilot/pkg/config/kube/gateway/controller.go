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

package gateway

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("gateway", "gateway-api controller", 0)

var (
	errUnsupportedOp   = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")
	errUnsupportedType = fmt.Errorf("unsupported type: this operation only supports gateway, destination rule, and virtual service resource type")
)

type Controller struct {
	client            kube.Client
	cache             model.ConfigStoreCache
	namespaceLister   listerv1.NamespaceLister
	namespaceInformer cache.SharedIndexInformer
	domain            string

	state   OutputResources
	stateMu sync.RWMutex

	statusEnabled *atomic.Bool
	status        status.WorkerQueue
}

var _ model.GatewayController = &Controller{}

func NewController(client kube.Client, c model.ConfigStoreCache, options controller2.Options) *Controller {
	var statusQueue status.WorkerQueue
	if features.EnableGatewayAPIStatus {
		statusQueue = status.NewWorkerPool(func(resource status.Resource, resourceStatus status.ResourceStatus) {
			log.Debugf("updating status for %v", resource.String())
			_, err := c.UpdateStatus(config.Config{
				// TODO stop round tripping this status.Resource<->config.Meta
				Meta:   status.ResourceToModelConfig(resource),
				Status: resourceStatus.(config.Status),
			})
			if err != nil {
				// TODO should we requeue or wait for another event to trigger an update?
				log.Errorf("failed to update status for %v/: %v", resource.String(), err)
			}
		}, uint(features.StatusMaxWorkers))
	}
	return &Controller{
		client:            client,
		cache:             c,
		namespaceLister:   client.KubeInformer().Core().V1().Namespaces().Lister(),
		namespaceInformer: client.KubeInformer().Core().V1().Namespaces().Informer(),
		domain:            options.DomainSuffix,
		status:            statusQueue,
		// Disabled by default, we will enable only if we win the leader election
		statusEnabled: atomic.NewBool(false),
	}
}

func (c *Controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.IstioNetworkingV1Alpha3Virtualservices,
		collections.IstioNetworkingV1Alpha3Gateways,
		collections.IstioNetworkingV1Alpha3Destinationrules,
	)
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

func deepCopyStatus(configs []config.Config) []config.Config {
	res := make([]config.Config, 0, len(configs))
	for _, c := range configs {
		nc := config.Config{
			Meta:   c.Meta,
			Spec:   c.Spec,
			Status: kstatus.Wrap(c.Status),
		}
		res = append(res, nc)
	}
	return res
}

func filterNamespace(cfgs []config.Config, namespace string) []config.Config {
	if namespace == metav1.NamespaceAll {
		return cfgs
	}
	filtered := make([]config.Config, 0, len(cfgs))
	for _, c := range cfgs {
		if c.Namespace == namespace {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	if typ != gvk.Gateway && typ != gvk.VirtualService && typ != gvk.DestinationRule {
		return nil, errUnsupportedType
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	switch typ {
	case gvk.Gateway:
		return filterNamespace(c.state.Gateway, namespace), nil
	case gvk.VirtualService:
		return filterNamespace(c.state.VirtualService, namespace), nil
	case gvk.DestinationRule:
		return filterNamespace(c.state.DestinationRule, namespace), nil
	default:
		return nil, errUnsupportedType
	}
}

func (c *Controller) SetStatusWrite(enabled bool) {
	c.statusEnabled.Store(enabled)
}

func (c *Controller) Recompute(context model.GatewayContext) error {
	t0 := time.Now()
	defer func() {
		log.Debugf("recompute complete in %v", time.Since(t0))
	}()
	gatewayClass, err := c.cache.List(gvk.GatewayClass, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type GatewayClass: %v", err)
	}
	gateway, err := c.cache.List(gvk.ServiceApisGateway, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type Gateway: %v", err)
	}
	httpRoute, err := c.cache.List(gvk.HTTPRoute, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type HTTPRoute: %v", err)
	}
	tcpRoute, err := c.cache.List(gvk.TCPRoute, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type TCPRoute: %v", err)
	}
	tlsRoute, err := c.cache.List(gvk.TLSRoute, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type TLSRoute: %v", err)
	}
	backendPolicy, err := c.cache.List(gvk.BackendPolicy, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type BackendPolicy: %v", err)
	}

	input := &KubernetesResources{
		GatewayClass:  deepCopyStatus(gatewayClass),
		Gateway:       deepCopyStatus(gateway),
		HTTPRoute:     deepCopyStatus(httpRoute),
		TCPRoute:      deepCopyStatus(tcpRoute),
		TLSRoute:      deepCopyStatus(tlsRoute),
		BackendPolicy: deepCopyStatus(backendPolicy),
		Domain:        c.domain,
		Context:       context,
	}

	if !anyApisUsed(input) {
		// Early exit for common case of no gateway-api used.
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		// make sure we clear out the state, to handle the last gateway-api resource being removed
		c.state = OutputResources{}
		return nil
	}

	nsl, err := c.namespaceLister.List(klabels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list type Namespaces: %v", err)
	}
	namespaces := map[string]*corev1.Namespace{}
	for _, ns := range nsl {
		namespaces[ns.Name] = ns
	}
	input.Namespaces = namespaces
	output := convertResources(input)

	// Handle all status updates
	c.QueueStatusUpdates(input)

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.state = output
	return nil
}

func (c *Controller) QueueStatusUpdates(r *KubernetesResources) {
	c.handleStatusUpdates(r.GatewayClass)
	c.handleStatusUpdates(r.Gateway)
	c.handleStatusUpdates(r.HTTPRoute)
	c.handleStatusUpdates(r.TCPRoute)
	c.handleStatusUpdates(r.TLSRoute)
	c.handleStatusUpdates(r.BackendPolicy)
}

func (c *Controller) handleStatusUpdates(configs []config.Config) {
	if c.status == nil || !c.statusEnabled.Load() {
		return
	}
	for _, cfg := range configs {
		ws := cfg.Status.(*kstatus.WrappedStatus)
		if ws.Dirty {
			res := status.ResourceFromModelConfig(cfg)
			c.status.Push(res, ws.Unwrap())
		}
	}
}

func anyApisUsed(input *KubernetesResources) bool {
	return len(input.GatewayClass) > 0 ||
		len(input.Gateway) > 0 ||
		len(input.HTTPRoute) > 0 ||
		len(input.TCPRoute) > 0 ||
		len(input.TLSRoute) > 0 ||
		len(input.BackendPolicy) > 0
}

func (c *Controller) Create(config config.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Update(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", errUnsupportedOp
}

func (c *Controller) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return errUnsupportedOp
}

func (c *Controller) RegisterEventHandler(typ config.GroupVersionKind, handler model.EventHandler) {
	// do nothing as c.cache has been registered
}

func (c *Controller) Run(stop <-chan struct{}) {
	cache.WaitForCacheSync(stop, c.namespaceInformer.HasSynced)
}

func (c *Controller) SetWatchErrorHandler(handler func(r *cache.Reflector, err error)) error {
	return c.cache.SetWatchErrorHandler(handler)
}

func (c *Controller) HasSynced() bool {
	return c.cache.HasSynced()
}
