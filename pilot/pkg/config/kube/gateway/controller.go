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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

var (
	errUnsupportedOp   = fmt.Errorf("unsupported operation: the gateway config store is a read-only view")
	errUnsupportedType = fmt.Errorf("unsupported type: this operation only supports gateway, destination rule, and virtual service resource type")
)

type controller struct {
	client kubernetes.Interface
	cache  model.ConfigStoreCache
	domain string
}

func NewController(client kubernetes.Interface, c model.ConfigStoreCache, options controller2.Options) model.ConfigStoreCache {
	return &controller{client, c, options.DomainSuffix}
}

func (c *controller) Schemas() collection.Schemas {
	return collection.SchemasFor(
		collections.IstioNetworkingV1Alpha3Virtualservices,
		collections.IstioNetworkingV1Alpha3Gateways,
		collections.IstioNetworkingV1Alpha3Destinationrules,
	)
}

func (c controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
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

func (c controller) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	if typ != gvk.Gateway && typ != gvk.VirtualService && typ != gvk.DestinationRule {
		return nil, errUnsupportedType
	}

	gatewayClass, err := c.cache.List(gvk.GatewayClass, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type GatewayClass: %v", err)
	}
	gateway, err := c.cache.List(gvk.ServiceApisGateway, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type Gateway: %v", err)
	}
	httpRoute, err := c.cache.List(gvk.HTTPRoute, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type HTTPRoute: %v", err)
	}
	tcpRoute, err := c.cache.List(gvk.TCPRoute, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type TCPRoute: %v", err)
	}
	tlsRoute, err := c.cache.List(gvk.TLSRoute, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type TLSRoute: %v", err)
	}
	backendPolicy, err := c.cache.List(gvk.BackendPolicy, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to list type BackendPolicy: %v", err)
	}

	input := &KubernetesResources{
		GatewayClass:  deepCopyStatus(gatewayClass),
		Gateway:       deepCopyStatus(gateway),
		HTTPRoute:     deepCopyStatus(httpRoute),
		TCPRoute:      deepCopyStatus(tcpRoute),
		TLSRoute:      deepCopyStatus(tlsRoute),
		BackendPolicy: deepCopyStatus(backendPolicy),
		Domain:        c.domain,
	}

	if !anyApisUsed(input) {
		// Early exit for common case of no gateway-api used.
		return nil, nil
	}

	nsl, err := c.client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list type Namespaces: %v", err)
	}
	namespaces := map[string]*corev1.Namespace{}
	for i, ns := range nsl.Items {
		namespaces[ns.Name] = &nsl.Items[i]
	}
	input.Namespaces = namespaces
	output := convertResources(input)

	// Handle all status updates
	// TODO we should probably place these on a queue using pilot/pkg/status
	input.UpdateStatuses(c)

	switch typ {
	case gvk.Gateway:
		return output.Gateway, nil
	case gvk.VirtualService:
		return output.VirtualService, nil
	case gvk.DestinationRule:
		return output.DestinationRule, nil
	}
	return nil, errUnsupportedOp
}

func (r *KubernetesResources) UpdateStatuses(c controller) {
	c.handleStatusUpdates(r.GatewayClass)
	c.handleStatusUpdates(r.Gateway)
	c.handleStatusUpdates(r.HTTPRoute)
	c.handleStatusUpdates(r.TCPRoute)
	c.handleStatusUpdates(r.TLSRoute)
	c.handleStatusUpdates(r.BackendPolicy)
}

func (c controller) handleStatusUpdates(configs []config.Config) {
	for _, cfg := range configs {
		ws := cfg.Status.(*kstatus.WrappedStatus)

		if ws.Dirty {
			_, err := c.cache.UpdateStatus(config.Config{
				Meta:   cfg.Meta,
				Status: ws.Unwrap(),
			})
			// TODO make this more resilient. When we add a queue we can make transient failures retry
			// and drop permanent failures
			if err != nil {
				log.Errorf("failed to update status for %v/%v: %v", cfg.GroupVersionKind, cfg.Name, err)
			}
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

func (c controller) Create(config config.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Update(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c controller) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", errUnsupportedOp
}

func (c controller) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return errUnsupportedOp
}

func (c controller) RegisterEventHandler(typ config.GroupVersionKind, handler func(config.Config, config.Config, model.Event)) {
	// do nothing as c.cache has been registered
}

func (c controller) Run(stop <-chan struct{}) {
}

func (c controller) SetWatchErrorHandler(handler func(r *cache.Reflector, err error)) error {
	return c.cache.SetWatchErrorHandler(handler)
}

func (c controller) HasSynced() bool {
	return c.cache.HasSynced()
}
