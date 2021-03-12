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

	"istio.io/istio/pilot/pkg/model"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
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
		GatewayClass:  gatewayClass,
		Gateway:       gateway,
		HTTPRoute:     httpRoute,
		TCPRoute:      tcpRoute,
		TLSRoute:      tlsRoute,
		BackendPolicy: backendPolicy,
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

func (c controller) HasSynced() bool {
	return c.cache.HasSynced()
}
