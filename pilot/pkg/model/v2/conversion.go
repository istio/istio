// Copyright 2018 Istio Authors
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

// Package v2 provides the adapters to convert Pilot's older data model objects
// to those required by envoy's v2 APIs.
// For Envoy terminology: https://www.envoyproxy.io/docs/envoy/latest/api-v2/api
//
// This package is likely to be deprecated once v1 REST based APIs are obsoleted.
// Use with extreme caution.
package v2

import (
	v2 "github.com/envoyproxy/go-control-plane/api"
	"istio.io/istio/pilot/pkg/model"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

// EndpointFromInstance returns an Envoy v2 Endpoint from Pilot's older data structure model.ServiceInstance.
func EndpointFromInstance(instance *model.ServiceInstance) *v2.LbEndpoint {
	labels := make([]envoyv2.EndpointLabel, 0, len(instance.Labels))
	for n, v := range instance.Labels {
		labels = append(labels, envoyv2.EndpointLabel{Name: n, Value: v})
	}
	// TODO: May need to handle errors. The probability of errors is low given that the older Pilot model does not use Istio destination labels
	// and all user supplied labels are single valued, given the older model stores them in a map.
	out, _ := envoyv2.NewEndpoint(instance.Service.Hostname, (uint32)(instance.Endpoint.Port), envoyv2.SocketProtocolTCP, labels)
	return (*v2.LbEndpoint)(out)
}

// EndpointFromInstance returns a list of Envoy v2 Endpoints from Pilot's older data structure model.ServiceInstance objects.
func EndpointsFromInstances(instances []*model.ServiceInstance) []*v2.LbEndpoint {
	out := make([]*v2.LbEndpoint, 0, len(instances))
	for _, instance := range instances {
		out = append(out, EndpointFromInstance(instance))
	}
	return out
}
