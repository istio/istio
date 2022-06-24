/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ServiceSelectorCache is a cache of service selectors to avoid high CPU consumption caused by frequent calls to AsSelectorPreValidated (see #73527)
type ServiceSelectorCache struct {
	lock  sync.RWMutex
	cache map[string]labels.Selector
}

// NewServiceSelectorCache init ServiceSelectorCache for both endpoint controller and endpointSlice controller.
func NewServiceSelectorCache() *ServiceSelectorCache {
	return &ServiceSelectorCache{
		cache: map[string]labels.Selector{},
	}
}

// Get return selector and existence in ServiceSelectorCache by key.
func (sc *ServiceSelectorCache) Get(key string) (labels.Selector, bool) {
	sc.lock.RLock()
	selector, ok := sc.cache[key]
	// fine-grained lock improves GetPodServiceMemberships performance(16.5%) than defer measured by BenchmarkGetPodServiceMemberships
	sc.lock.RUnlock()
	return selector, ok
}

// Update can update or add a selector in ServiceSelectorCache while service's selector changed.
func (sc *ServiceSelectorCache) Update(key string, rawSelector map[string]string) labels.Selector {
	sc.lock.Lock()
	defer sc.lock.Unlock()
	selector := labels.Set(rawSelector).AsSelectorPreValidated()
	sc.cache[key] = selector
	return selector
}

// Delete can delete selector which exist in ServiceSelectorCache.
func (sc *ServiceSelectorCache) Delete(key string) {
	sc.lock.Lock()
	defer sc.lock.Unlock()
	delete(sc.cache, key)
}

// GetPodServiceMemberships returns a set of Service keys for Services that have
// a selector matching the given pod.
func (sc *ServiceSelectorCache) GetPodServiceMemberships(serviceLister v1listers.ServiceLister, pod *v1.Pod) ([]*v1.Service, error) {
	var services []*v1.Service
	services, err := serviceLister.Services(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var selector labels.Selector
	for _, service := range services {
		if service.Spec.Selector == nil {
			// if the service has a nil selector this means selectors match nothing, not everything.
			continue
		}
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(service)
		if err != nil {
			return nil, err
		}
		if v, ok := sc.Get(key); ok {
			selector = v
		} else {
			selector = sc.Update(key, service.Spec.Selector)
		}

		if selector.Matches(labels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}
	return services, nil
}