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

package ingress

import (
	"sync"

	knetworking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/util/sets"
)

// serviceStore stores the services that are of port name type, specified by ingresses.
type serviceStore struct {
	sync.RWMutex
	// servicesByIngress records services that are of port name type, specified by the map key ingress.
	// ingress's namespaced name -> services' namespaced name collection.
	servicesByIngress map[string]sets.String
	// ingressesByService records ingresses that refer to the map key service that are of port name type.
	// service's namespaced name -> ingresses' namespaced name collection.
	ingressesByService map[string]sets.String
}

func newServiceStore() *serviceStore {
	return &serviceStore{
		servicesByIngress:  make(map[string]sets.String),
		ingressesByService: make(map[string]sets.String),
	}
}

// ingressUpdated should be called when we receive added or updated event of ingress resources.
func (s *serviceStore) ingressUpdated(ingress *knetworking.Ingress) {
	s.Lock()
	defer s.Unlock()

	namespacedName := config.NamespacedName(ingress).String()
	old := s.servicesByIngress[namespacedName]
	cur := extractServicesByPortNameType(ingress)
	s.servicesByIngress[namespacedName] = cur
	s.updateIngressesByService(old, cur, namespacedName)
}

// ingressDeleted should be called when we receive deleted event of ingress resources.
// Additionally, If this ingress wouldn't be processed by istio which can happen in the case of
// this ingress's class has been changed, this method should be also invoked.
func (s *serviceStore) ingressDeleted(ingress *knetworking.Ingress) {
	s.Lock()
	defer s.Unlock()

	namespacedName := config.NamespacedName(ingress).String()
	old, exist := s.servicesByIngress[namespacedName]
	if !exist {
		return
	}
	delete(s.servicesByIngress, namespacedName)
	s.updateIngressesByService(old, nil, namespacedName)
}

// contains will return true if the specified service is referred by ingress.
func (s *serviceStore) contains(service string) bool {
	s.RLock()
	defer s.RUnlock()

	_, exist := s.ingressesByService[service]
	return exist
}

// This method is not thread-safe and can only be directly called by ingressUpdated or ingressDeleted.
// Users should not use this method doing something else.
func (s *serviceStore) updateIngressesByService(old, cur sets.String, ingress string) {
	deleted, added := old.Diff(cur)
	for _, item := range deleted {
		s.ingressesByService[item].Delete(ingress)
		if len(s.ingressesByService[item]) == 0 {
			// There are no ingresses refer this service, so we remove the service from map.
			delete(s.ingressesByService, item)
		}
	}
	for _, item := range added {
		if s.ingressesByService[item] == nil {
			s.ingressesByService[item] = sets.String{}
		}
		s.ingressesByService[item].Insert(ingress)
	}
}

// extractServicesByPortNameType extract services that are of port name type in the specified ingress resource.
func extractServicesByPortNameType(ingress *knetworking.Ingress) sets.String {
	services := sets.String{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		for _, route := range rule.HTTP.Paths {
			if route.Backend.Service == nil {
				continue
			}

			if route.Backend.Service.Port.Name != "" {
				services.Insert(types.NamespacedName{
					Namespace: ingress.GetNamespace(),
					Name:      route.Backend.Service.Name,
				}.String())
			}
		}
	}
	return services
}
