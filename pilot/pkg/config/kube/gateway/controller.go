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

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("gateway", "gateway-api controller", 0)

var (
	errUnsupportedOp   = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")
	errUnsupportedType = fmt.Errorf("unsupported type: this operation only supports gateway and virtual service resource type")
)

// Controller defines the controller for the gateway-api. The controller acts a bit different from most.
// Rather than watching the CRs directly, we depend on the existing model.ConfigStoreController which
// already watches all CRs. When there are updates, a new PushContext will be computed, which will eventually
// call Controller.Recompute(). Once this happens, we will inspect the current state of the world, and transform
// gateway-api types into Istio types (Gateway/VirtualService). Future calls to Get/List will return these
// Istio types. These are not stored in the cluster at all, and are purely internal; they can be seen on /debug/configz.
// During Recompute(), the status on all gateway-api types is also tracked. Once completed, if the status
// has changed at all, it is queued to asynchronously update the status of the object in Kubernetes.
type Controller struct {
	// client for accessing Kubernetes
	client kube.Client
	// cache provides access to the underlying gateway-configs
	cache model.ConfigStoreController

	// Gateway-api types reference namespace labels directly, so we need access to these
	namespaceLister   listerv1.NamespaceLister
	namespaceInformer cache.SharedIndexInformer
	namespaceHandler  model.EventHandler

	// domain stores the cluster domain, typically cluster.local
	domain string

	// state is our computed Istio resources. Access is guarded by stateMu. This is updated from Recompute().
	state   OutputResources
	stateMu sync.RWMutex

	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	statusController *status.Controller
	statusEnabled    *atomic.Bool
}

var _ model.GatewayController = &Controller{}

func NewController(client kube.Client, c model.ConfigStoreController, options controller.Options) *Controller {
	var ctl *status.Controller

	nsInformer := client.KubeInformer().Core().V1().Namespaces().Informer()
	gatewayController := &Controller{
		client:            client,
		cache:             c,
		namespaceLister:   client.KubeInformer().Core().V1().Namespaces().Lister(),
		namespaceInformer: nsInformer,
		domain:            options.DomainSuffix,
		statusController:  ctl,
		// Disabled by default, we will enable only if we win the leader election
		statusEnabled: atomic.NewBool(false),
	}

	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			gatewayController.namespaceEvent(nil, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			gatewayController.namespaceEvent(oldObj, newObj)
		},
	})

	return gatewayController
}

func (c *Controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.IstioNetworkingV1Alpha3Virtualservices,
		collections.IstioNetworkingV1Alpha3Gateways,
	)
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	if typ != gvk.Gateway && typ != gvk.VirtualService {
		return nil, errUnsupportedType
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	switch typ {
	case gvk.Gateway:
		return filterNamespace(c.state.Gateway, namespace), nil
	case gvk.VirtualService:
		return filterNamespace(c.state.VirtualService, namespace), nil
	default:
		return nil, errUnsupportedType
	}
}

func (c *Controller) SetStatusWrite(enabled bool, statusManager *status.Manager) {
	c.statusEnabled.Store(enabled)
	if enabled && features.EnableGatewayAPIStatus && statusManager != nil {
		c.statusController = statusManager.CreateGenericController(func(status interface{}, context interface{}) status.GenerationProvider {
			return &gatewayGeneration{context}
		})
	} else {
		c.statusController = nil
	}
}

// Recompute takes in a current snapshot of the gateway-api configs, and regenerates our internal state.
// Any status updates required will be enqueued as well.
func (c *Controller) Recompute(context model.GatewayContext) error {
	t0 := time.Now()
	defer func() {
		log.Debugf("recompute complete in %v", time.Since(t0))
	}()
	gatewayClass, err := c.cache.List(gvk.GatewayClass, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type GatewayClass: %v", err)
	}
	gateway, err := c.cache.List(gvk.KubernetesGateway, metav1.NamespaceAll)
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
	referencePolicy, err := c.cache.List(gvk.ReferencePolicy, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type BackendPolicy: %v", err)
	}
	referenceGrant, err := c.cache.List(gvk.ReferenceGrant, metav1.NamespaceAll)
	if err != nil {
		return fmt.Errorf("failed to list type BackendPolicy: %v", err)
	}

	input := KubernetesResources{
		GatewayClass:    deepCopyStatus(gatewayClass),
		Gateway:         deepCopyStatus(gateway),
		HTTPRoute:       deepCopyStatus(httpRoute),
		TCPRoute:        deepCopyStatus(tcpRoute),
		TLSRoute:        deepCopyStatus(tlsRoute),
		ReferencePolicy: referencePolicy,
		ReferenceGrant:  referenceGrant,
		Domain:          c.domain,
		Context:         context,
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

func (c *Controller) QueueStatusUpdates(r KubernetesResources) {
	c.handleStatusUpdates(r.GatewayClass)
	c.handleStatusUpdates(r.Gateway)
	c.handleStatusUpdates(r.HTTPRoute)
	c.handleStatusUpdates(r.TCPRoute)
	c.handleStatusUpdates(r.TLSRoute)
}

func (c *Controller) handleStatusUpdates(configs []config.Config) {
	if c.statusController == nil || !c.statusEnabled.Load() {
		return
	}
	for _, cfg := range configs {
		ws := cfg.Status.(*kstatus.WrappedStatus)
		if ws.Dirty {
			res := status.ResourceFromModelConfig(cfg)
			c.statusController.EnqueueStatusUpdateResource(ws.Unwrap(), res)
		}
	}
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
	switch typ {
	case gvk.Namespace:
		c.namespaceHandler = handler
	}
	// For all other types, do nothing as c.cache has been registered
}

func (c *Controller) Run(stop <-chan struct{}) {
	go func() {
		if crdclient.WaitForCRD(gvk.GatewayClass, stop) {
			gcc := NewClassController(c.client)
			c.client.RunAndWait(stop)
			gcc.Run(stop)
		}
	}()
	kube.WaitForCacheSync(stop, c.namespaceInformer.HasSynced)
}

func (c *Controller) SetWatchErrorHandler(handler func(r *cache.Reflector, err error)) error {
	return c.cache.SetWatchErrorHandler(handler)
}

func (c *Controller) HasSynced() bool {
	return c.cache.HasSynced()
}

func (c *Controller) SecretAllowed(resourceName string, namespace string) bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state.AllowedReferences.SecretAllowed(resourceName, namespace)
}

// namespaceEvent handles a namespace add/update. Gateway's can select routes by label, so we need to handle
// when the labels change.
// Note: we don't handle delete as a delete would also clean up any relevant gateway-api types which will
// trigger its own event.
func (c *Controller) namespaceEvent(oldObj interface{}, newObj interface{}) {
	// First, find all the label keys on the old/new namespace. We include NamespaceNameLabel
	// since we have special logic to always allow this on namespace.
	touchedNamespaceLabels := sets.New(NamespaceNameLabel)
	touchedNamespaceLabels.InsertAll(getLabelKeys(oldObj)...)
	touchedNamespaceLabels.InsertAll(getLabelKeys(newObj)...)

	// Next, we find all keys our Gateways actually reference.
	c.stateMu.RLock()
	intersection := touchedNamespaceLabels.Intersection(c.state.ReferencedNamespaceKeys)
	c.stateMu.RUnlock()

	// If there was any overlap, then a relevant namespace label may have changed, and we trigger a
	// push A more exact check could actually determine if the label selection result actually changed.
	// However, this is a much simpler approach that is likely to scale well enough for now.
	if !intersection.IsEmpty() && c.namespaceHandler != nil {
		log.Debugf("namespace labels changed, triggering namespace handler: %v", intersection.UnsortedList())
		c.namespaceHandler(config.Config{}, config.Config{}, model.EventUpdate)
	}
}

// getLabelKeys extracts all label keys from a namespace object.
func getLabelKeys(obj interface{}) []string {
	if obj == nil {
		return nil
	}
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil
		}
		ns, ok = tombstone.Obj.(*corev1.Namespace)
		if !ok {
			return nil
		}
	}
	keys := make([]string, 0, len(ns.Labels))
	for k := range ns.Labels {
		keys = append(keys, k)
	}
	return keys
}

// deepCopyStatus creates a copy of all configs, with a copy of the status field that we can mutate.
// This allows our functions to call Status.Mutate, and then we can later persist all changes into the
// API server.
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

// filterNamespace allows filtering out configs to only a specific namespace. This allows implementing the
// List call which can specify a specific namespace.
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

// anyApisUsed determines if there are any gateway-api resources created at all. If not, we can
// short circuit all processing to avoid excessive work.
func anyApisUsed(input KubernetesResources) bool {
	return len(input.GatewayClass) > 0 ||
		len(input.Gateway) > 0 ||
		len(input.HTTPRoute) > 0 ||
		len(input.TCPRoute) > 0 ||
		len(input.TLSRoute) > 0 ||
		len(input.ReferencePolicy) > 0
}
