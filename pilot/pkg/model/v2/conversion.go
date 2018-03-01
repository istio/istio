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
	"strconv"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"

	"istio.io/istio/pilot/pkg/model"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/log"
)

// EndpointFromInstance returns an Envoy v2 Endpoint from Pilot's older data structure model.ServiceInstance.
func EndpointFromInstance(instance *model.ServiceInstance) (*endpoint.LbEndpoint, error) {
	labels := make([]envoyv2.EndpointLabel, 0, len(instance.Labels)+2)
	for n, v := range instance.Labels {
		labels = append(labels, envoyv2.EndpointLabel{Name: n, Value: v})
	}
	// TODO: remove the following comment once Envoy's REST APIs are deprecated in Istio.
	// The following labels will be inconsequentil to Envoy, but is forward compatible with Istio's use of Envoy v2 APIs
	// particularly in Istio environments involving remote Pilot discovery.
	epUID := instance.Service.Hostname + "|" + instance.Endpoint.Address + ":" + strconv.Itoa(instance.Endpoint.Port)
	labels = append(labels,
		envoyv2.EndpointLabel{Name: envoyv2.DestinationUID.AttrName(), Value: epUID},
		envoyv2.EndpointLabel{Name: envoyv2.DestinationService.AttrName(), Value: instance.Service.Hostname})
	out, err := envoyv2.NewEndpoint(instance.Endpoint.Address, (uint32)(instance.Endpoint.Port), envoyv2.SocketProtocolTCP, labels)
	return (*endpoint.LbEndpoint)(out), err
}

// LocalityLbEndpointsFromInstances returns a list of Envoy v2 LocalityLbEndpoints and a total count of
// Envoy v2 Endpoints constructed from Pilot's older data structure involving model.ServiceInstance objects.
func LocalityLbEndpointsFromInstances(instances []*model.ServiceInstance) []endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		lbEp, err := EndpointFromInstance(instance)
		if err != nil {
			log.Errorf("unexpected pilot model endpoint v1 to v2 conversion: %v", err)
			continue
		}
		// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
		// Once we do that, the key must be a | separated tupple.
		locality := instance.AvailabilityZone
		locLbEps, found := localityEpMap[locality]
		if !found {
			locLbEps = endpoint.LocalityLbEndpoints{
				Locality: &core.Locality{
					Zone: instance.AvailabilityZone,
				},
			}
			localityEpMap[locality] = locLbEps
		}
		locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, *lbEp)
	}
	out := make([]endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		out = append(out, locLbEps)
	}
	return out
}
