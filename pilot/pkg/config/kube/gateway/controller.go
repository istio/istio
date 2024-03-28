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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("gateway", "gateway-api controller")

var errUnsupportedOp = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")

// Controller defines the controller for the gateway-api. The controller acts a bit different from most.
// Rather than watching the CRs directly, we depend on the existing model.ConfigStoreController which
// already watches all CRs. When there are updates, a new PushContext will be computed, which will eventually
// call Controller.Reconcile(). Once this happens, we will inspect the current state of the world, and transform
// gateway-api types into Istio types (Gateway/VirtualService). Future calls to Get/List will return these
// Istio types. These are not stored in the cluster at all, and are purely internal; they can be seen on /debug/configz.
// During Reconcile(), the status on all gateway-api types is also tracked. Once completed, if the status
// has changed at all, it is queued to asynchronously update the status of the object in Kubernetes.
type Controller struct {
	// client for accessing Kubernetes
	client kube.Client
	// cache provides access to the underlying gateway-configs
	cache model.ConfigStoreController

	// Gateway-api types reference namespace labels directly, so we need access to these
	namespaces       kclient.Client[*corev1.Namespace]
	namespaceHandler model.EventHandler

	// Gateway-api types reference secrets directly, so we need access to these
	credentialsController credentials.MulticlusterController
	secretHandler         model.EventHandler

	// the cluster where the gateway-api controller runs
	cluster cluster.ID
	// domain stores the cluster domain, typically cluster.local
	domain string

	// state is our computed Istio resources. Access is guarded by stateMu. This is updated from Reconcile().
	state   IstioResources
	stateMu sync.RWMutex

	// statusController controls the status working queue. Status will only be written if statusEnabled is true, which
	// is only the case when we are the leader.
	statusController *atomic.Pointer[status.Controller]

	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool
}

var _ model.GatewayController = &Controller{}

func NewController(
	kc kube.Client,
	c model.ConfigStoreController,
	waitForCRD func(class schema.GroupVersionResource, stop <-chan struct{}) bool,
	credsController credentials.MulticlusterController,
	options controller.Options,
) *Controller {
	var ctl *status.Controller

	namespaces := kclient.NewFiltered[*corev1.Namespace](kc, kubetypes.Filter{ObjectFilter: kc.ObjectFilter()})
	gatewayController := &Controller{
		client:                kc,
		cache:                 c,
		namespaces:            namespaces,
		credentialsController: credsController,
		cluster:               options.ClusterID,
		domain:                options.DomainSuffix,
		statusController:      atomic.NewPointer(ctl),
		waitForCRD:            waitForCRD,
	}

	namespaces.AddEventHandler(controllers.EventHandler[*corev1.Namespace]{
		UpdateFunc: func(oldNs, newNs *corev1.Namespace) {
			if !labels.Instance(oldNs.Labels).Equals(newNs.Labels) {
				gatewayController.namespaceEvent(oldNs, newNs)
			}
		},
	})

	if credsController != nil {
		credsController.AddSecretHandler(gatewayController.secretEvent)
	}

	return gatewayController
}

func (c *Controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.VirtualService,
		collections.Gateway,
	)
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if typ != gvk.Gateway && typ != gvk.VirtualService {
		return nil
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	switch typ {
	case gvk.Gateway:
		return filterNamespace(c.state.Gateway, namespace)
	case gvk.VirtualService:
		return filterNamespace(c.state.VirtualService, namespace)
	default:
		return nil
	}
}

func (c *Controller) SetStatusWrite(enabled bool, statusManager *status.Manager) {
	if enabled && features.EnableGatewayAPIStatus && statusManager != nil {
		c.statusController.Store(
			statusManager.CreateGenericController(func(status any, context any) status.GenerationProvider {
				return &gatewayGeneration{context}
			}),
		)
	} else {
		c.statusController.Store(nil)
	}
}

// Reconcile takes in a current snapshot of the gateway-api configs, and regenerates our internal state.
// Any status updates required will be enqueued as well.
func (c *Controller) Reconcile(ps *model.PushContext) error {
	t0 := time.Now()
	defer func() {
		log.Debugf("reconcile complete in %v", time.Since(t0))
	}()
	gatewayClass := c.cache.List(gvk.GatewayClass, metav1.NamespaceAll)
	gateway := c.cache.List(gvk.KubernetesGateway, metav1.NamespaceAll)
	httpRoute := c.cache.List(gvk.HTTPRoute, metav1.NamespaceAll)
	grpcRoute := c.cache.List(gvk.GRPCRoute, metav1.NamespaceAll)
	tcpRoute := c.cache.List(gvk.TCPRoute, metav1.NamespaceAll)
	tlsRoute := c.cache.List(gvk.TLSRoute, metav1.NamespaceAll)
	referenceGrant := c.cache.List(gvk.ReferenceGrant, metav1.NamespaceAll)
	serviceEntry := c.cache.List(gvk.ServiceEntry, metav1.NamespaceAll) // TODO lazy load only referenced SEs?

	input := GatewayResources{
		GatewayClass:   deepCopyStatus(gatewayClass),
		Gateway:        deepCopyStatus(gateway),
		HTTPRoute:      deepCopyStatus(httpRoute),
		GRPCRoute:      deepCopyStatus(grpcRoute),
		TCPRoute:       deepCopyStatus(tcpRoute),
		TLSRoute:       deepCopyStatus(tlsRoute),
		ReferenceGrant: referenceGrant,
		ServiceEntry:   serviceEntry,
		Domain:         c.domain,
		Context:        NewGatewayContext(ps, c.cluster),
	}

	if !input.hasResources() {
		// Early exit for common case of no gateway-api used.
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		// make sure we clear out the state, to handle the last gateway-api resource being removed
		c.state = IstioResources{}
		return nil
	}

	nsl := c.namespaces.List("", klabels.Everything())
	namespaces := make(map[string]*corev1.Namespace, len(nsl))
	for _, ns := range nsl {
		namespaces[ns.Name] = ns
	}
	input.Namespaces = namespaces

	if c.credentialsController != nil {
		credentials, err := c.credentialsController.ForCluster(c.cluster)
		if err != nil {
			return fmt.Errorf("failed to get credentials: %v", err)
		}
		input.Credentials = credentials
	}

	output := convertResources(input)

	// Handle all status updates
	c.QueueStatusUpdates(input)

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.state = output
	return nil
}

func (c *Controller) QueueStatusUpdates(r GatewayResources) {
	c.handleStatusUpdates(r.GatewayClass)
	c.handleStatusUpdates(r.Gateway)
	c.handleStatusUpdates(r.HTTPRoute)
	c.handleStatusUpdates(r.GRPCRoute)
	c.handleStatusUpdates(r.TCPRoute)
	c.handleStatusUpdates(r.TLSRoute)
}

func (c *Controller) handleStatusUpdates(configs []config.Config) {
	statusController := c.statusController.Load()
	if statusController == nil {
		return
	}
	for _, cfg := range configs {
		ws := cfg.Status.(*kstatus.WrappedStatus)
		if ws.Dirty {
			res := status.ResourceFromModelConfig(cfg)
			statusController.EnqueueStatusUpdateResource(ws.Unwrap(), res)
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
	case gvk.Secret:
		c.secretHandler = handler
	}
	// For all other types, do nothing as c.cache has been registered
}

func (c *Controller) Run(stop <-chan struct{}) {
	if features.EnableGatewayAPIGatewayClassController {
		go func() {
			if c.waitForCRD(gvr.GatewayClass, stop) {
				gcc := NewClassController(c.client)
				c.client.RunAndWait(stop)
				gcc.Run(stop)
			}
		}()
	}
}

func (c *Controller) HasSynced() bool {
	return c.cache.HasSynced() && c.namespaces.HasSynced()
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
func (c *Controller) namespaceEvent(oldNs, newNs *corev1.Namespace) {
	// First, find all the label keys on the old/new namespace. We include NamespaceNameLabel
	// since we have special logic to always allow this on namespace.
	touchedNamespaceLabels := sets.New(NamespaceNameLabel)
	touchedNamespaceLabels.InsertAll(getLabelKeys(oldNs)...)
	touchedNamespaceLabels.InsertAll(getLabelKeys(newNs)...)

	// Next, we find all keys our Gateways actually reference.
	c.stateMu.RLock()
	intersection := touchedNamespaceLabels.Intersection(c.state.ReferencedNamespaceKeys)
	c.stateMu.RUnlock()

	// If there was any overlap, then a relevant namespace label may have changed, and we trigger a
	// push. A more exact check could actually determine if the label selection result actually changed.
	// However, this is a much simpler approach that is likely to scale well enough for now.
	if !intersection.IsEmpty() && c.namespaceHandler != nil {
		log.Debugf("namespace labels changed, triggering namespace handler: %v", intersection.UnsortedList())
		c.namespaceHandler(config.Config{}, config.Config{}, model.EventUpdate)
	}
}

// getLabelKeys extracts all label keys from a namespace object.
func getLabelKeys(ns *corev1.Namespace) []string {
	if ns == nil {
		return nil
	}
	return maps.Keys(ns.Labels)
}

func (c *Controller) secretEvent(name, namespace string) {
	var impactedConfigs []model.ConfigKey
	c.stateMu.RLock()
	impactedConfigs = c.state.ResourceReferences[model.ConfigKey{
		Kind:      kind.Secret,
		Namespace: namespace,
		Name:      name,
	}]
	c.stateMu.RUnlock()
	if len(impactedConfigs) > 0 {
		log.Debugf("secret %s/%s changed, triggering secret handler", namespace, name)
		for _, cfg := range impactedConfigs {
			gw := config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.KubernetesGateway,
					Namespace:        cfg.Namespace,
					Name:             cfg.Name,
				},
			}
			c.secretHandler(gw, gw, model.EventUpdate)
		}
	}
}

// deepCopyStatus creates a copy of all configs, with a copy of the status field that we can mutate.
// This allows our functions to call Status.Mutate, and then we can later persist all changes into the
// API server.
func deepCopyStatus(configs []config.Config) []config.Config {
	return slices.Map(configs, func(c config.Config) config.Config {
		return config.Config{
			Meta:   c.Meta,
			Spec:   c.Spec,
			Status: kstatus.Wrap(c.Status),
		}
	})
}

// filterNamespace allows filtering out configs to only a specific namespace. This allows implementing the
// List call which can specify a specific namespace.
func filterNamespace(cfgs []config.Config, namespace string) []config.Config {
	if namespace == metav1.NamespaceAll {
		return cfgs
	}
	return slices.Filter(cfgs, func(c config.Config) bool {
		return c.Namespace == namespace
	})
}

// hasResources determines if there are any gateway-api resources created at all.
// If not, we can short circuit all processing to avoid excessive work.
func (kr GatewayResources) hasResources() bool {
	return len(kr.GatewayClass) > 0 ||
		len(kr.Gateway) > 0 ||
		len(kr.HTTPRoute) > 0 ||
		len(kr.GRPCRoute) > 0 ||
		len(kr.TCPRoute) > 0 ||
		len(kr.TLSRoute) > 0 ||
		len(kr.ReferenceGrant) > 0
}
