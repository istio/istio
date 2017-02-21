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

// TODO: describes what this module is for and comments the methods below.

package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

// TODO: check whether this resync period is reasonable.
const resyncPeriod = 10 * time.Second

type SecureNamingController struct {
	serviceController cache.Controller
	serviceIndexer    cache.Indexer
}

func NewSecureNamingController(core v1core.CoreV1Interface) *SecureNamingController {
	si, sc := newServiceIndexerInformer(core)
	return &SecureNamingController{sc, si}
}

func newServiceIndexerInformer(core v1core.CoreV1Interface) (cache.Indexer, cache.Controller) {
	si := core.Services(v1.NamespaceAll)
	return cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return si.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return si.Watch(options)
			},
		},
		&v1.Service{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
}

func (snc *SecureNamingController) getPodServices(pod *v1.Pod) []*v1.Service {
	allServices := []*v1.Service{}
	cache.ListAllByNamespace(snc.serviceIndexer, pod.Namespace, labels.Everything(), func(m interface{}) {
		allServices = append(allServices, m.(*v1.Service))
	})

	services := []*v1.Service{}
	for _, service := range allServices {
		if service.Spec.Selector == nil {
			continue
		}
		selector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(labels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}
	return services
}
