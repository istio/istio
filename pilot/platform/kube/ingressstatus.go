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
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress/core/pkg/ingress/status"
	"k8s.io/ingress/core/pkg/ingress/store"

	proxyconfig "istio.io/api/proxy/v1/config"
)

// IngressStatusSyncer keeps the status IP in each Ingress resource updated
type IngressStatusSyncer struct {
	sync     status.Sync
	informer cache.SharedIndexInformer
}

// Run the syncer until stopCh is closed
func (s *IngressStatusSyncer) Run(stopCh <-chan struct{}) {
	go func() {
		s.sync.Run(stopCh)
		s.sync.Shutdown()
	}()
	go s.informer.Run(stopCh)
	<-stopCh
}

// NewIngressStatusSyncer creates a new instance
func NewIngressStatusSyncer(mesh *proxyconfig.ProxyMeshConfig, client *Client,
	options ControllerOptions) *IngressStatusSyncer {

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				return client.client.ExtensionsV1beta1().Ingresses(options.Namespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.client.ExtensionsV1beta1().Ingresses(options.Namespace).Watch(opts)
			},
		},
		&v1beta1.Ingress{}, options.ResyncPeriod, cache.Indexers{},
	)

	var publishService string
	if mesh.IngressService != "" {
		publishService = fmt.Sprintf("%v/%v", options.Namespace, mesh.IngressService)
	}
	ingressClass, defaultIngressClass := convertIngressControllerMode(mesh.IngressControllerMode, mesh.IngressClass)
	sync := status.NewStatusSyncer(status.Config{
		Client:              client.GetKubernetesClient(),
		IngressLister:       store.IngressLister{Store: informer.GetStore()},
		ElectionID:          "ingress-controller-leader", // TODO: configurable?
		PublishService:      publishService,
		DefaultIngressClass: defaultIngressClass,
		IngressClass:        ingressClass,
	})

	return &IngressStatusSyncer{
		sync:     sync,
		informer: informer,
	}
}

// convertIngressControllerMode converts Ingress controller mode into k8s ingress status syncer ingress class and
// default ingress class. Ingress class and default ingress class are used by the syncer to determine whether or not to
// update the IP of a ingress resource.
func convertIngressControllerMode(mode proxyconfig.ProxyMeshConfig_IngressControllerMode,
	class string) (string, string) {
	var ingressClass, defaultIngressClass string
	switch mode {
	case proxyconfig.ProxyMeshConfig_DEFAULT:
		defaultIngressClass = class
		ingressClass = class
	case proxyconfig.ProxyMeshConfig_STRICT:
		ingressClass = class
	}
	return ingressClass, defaultIngressClass
}
