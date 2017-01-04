// Copyright 2016 Google Inc.
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

package model

// ServiceDiscovery enumerates Istio service instances
type ServiceDiscovery interface {
	// Services list all services and their tags
	Services() []*Service
	// Endpoints retrieves service instances for a service and a tag
	Endpoints(s *Service, tag string) []*ServiceInstance
}

// Service describes an Istio service
type Service struct {
	// Name of the service
	Name string `json:"name,omitempty"`
	// Namespace of the service name
	Namespace string `json:"namespace,omitempty"`
	// Tags is a set of declared tags for the service.
	// An empty set is allowed but tags must be non-empty strings.
	Tags []string `json:"tags,omitempty"`
}

// Endpoint defines a network endpoint
type Endpoint struct {
	// Address of the endpoint, typically an IP address
	Address string `json:"ip_address,omitempty"`
	// Port on the host address
	Port int `json:"port,omitempty"`
}

// ServiceInstance binds an endpoint to a service and a tag.
// If the service has no tags, the tag value is an empty string;
// otherwise, the tag value is an element in the set of service tags.
type ServiceInstance struct {
	Endpoint Endpoint `json:"endpoint,omitempty"`
	Service  Service  `json:"service,omitempty"`
	Tag      string   `json:"tag,omitempty"`
}
