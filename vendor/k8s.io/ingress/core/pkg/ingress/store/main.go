/*
Copyright 2015 The Kubernetes Authors.

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

package store

import (
	"fmt"

	api "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

// IngressLister makes a Store that lists Ingress.
type IngressLister struct {
	cache.Store
}

// SecretsLister makes a Store that lists Secrets.
type SecretsLister struct {
	cache.Store
}

// ConfigMapLister makes a Store that lists Configmaps.
type ConfigMapLister struct {
	cache.Store
}

// ServiceLister makes a Store that lists Services.
type ServiceLister struct {
	cache.Store
}

// NodeLister makes a Store that lists Nodes.
type NodeLister struct {
	cache.Store
}

// EndpointLister makes a Store that lists Endpoints.
type EndpointLister struct {
	cache.Store
}

// GetServiceEndpoints returns the endpoints of a service, matched on service name.
func (s *EndpointLister) GetServiceEndpoints(svc *api.Service) (ep api.Endpoints, err error) {
	for _, m := range s.Store.List() {
		ep = *m.(*api.Endpoints)
		if svc.Name == ep.Name && svc.Namespace == ep.Namespace {
			return ep, nil
		}
	}
	err = fmt.Errorf("could not find endpoints for service: %v", svc.Name)
	return
}

// SecretLister makes a Store that lists Secres.
type SecretLister struct {
	cache.Store
}
