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

package v2

import (
	"errors"
	"net"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/proxy/envoy/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// Endpoints implements MeshDiscovery.Endpoints()
func Endpoints(ds *v1.DiscoveryService, serviceClusters []string) *xdsapi.DiscoveryResponse {
	// Not using incCounters/observeResources: grpc has an interceptor for prometheus.
	version := time.Now().String()
	clAssignment := &xdsapi.ClusterLoadAssignment{}
	clAssignmentRes, _ := types.MarshalAny(clAssignment)
	out := &xdsapi.DiscoveryResponse{
		// All resources for EDS ought to be of the type ClusterLoadAssignment
		TypeUrl: clAssignmentRes.GetTypeUrl(),

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject EDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: version,
		Nonce:       version,
	}

	var totalEndpoints uint32
	out.Resources = make([]types.Any, 0, len(serviceClusters))
	for _, serviceCluster := range serviceClusters {
		hostname, ports, labels := model.ParseServiceKey(serviceCluster)
		instances, err := ds.Instances(hostname, ports.GetNames(), labels)
		if err != nil {
			log.Warnf("endpoints for service cluster %q returned error %q", serviceCluster, err)
			continue
		}
		locEps := localityLbEndpointsFromInstances(instances)
		if len(instances) == 0 {
			log.Infoa("EDS: no instances ", serviceCluster, hostname, ports, labels)
		}
		clAssignment := &xdsapi.ClusterLoadAssignment{
			ClusterName: serviceCluster,
			Endpoints:   locEps,
		}
		//log.Infof("EDS: %v %s", serviceCluster, clAssignment.String())
		clAssignmentRes, _ = types.MarshalAny(clAssignment)
		out.Resources = append(out.Resources, *clAssignmentRes)
		totalEndpoints += uint32(len(locEps))
	}

	return out
}

func newEndpoint(address string, port uint32) (*endpoint.LbEndpoint, error) {
	ipAddr := net.ParseIP(address)
	if ipAddr == nil {
		return nil, errors.New("Invalid IP address " + address)
	}
	ep := &endpoint.LbEndpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					&core.SocketAddress{
						Address:    address,
						Ipv4Compat: ipAddr.To4() != nil,
						Protocol:   core.TCP,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: port,
						},
					},
				},
			},
		},
	}

	//log.Infoa("EDS: endpoint ", ipAddr, ep.String())
	return ep, nil
}

// LocalityLbEndpointsFromInstances returns a list of Envoy v2 LocalityLbEndpoints.
// Envoy v2 Endpoints are constructed from Pilot's older data structure involving
// model.ServiceInstance objects. Envoy expects the endpoints grouped by zone, so
// a map is created - in new data structures this should be part of the model.
func localityLbEndpointsFromInstances(instances []*model.ServiceInstance) []endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		lbEp, err := newEndpoint(instance.Endpoint.Address, (uint32)(instance.Endpoint.Port))
		if err != nil {
			log.Errorf("EDS: unexpected pilot model endpoint v1 to v2 conversion: %v", err)
			continue
		}
		// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
		// Once we do that, the key must be a | separated tupple.
		locality := instance.AvailabilityZone
		locLbEps, found := localityEpMap[locality]
		if !found {
			locLbEps = &endpoint.LocalityLbEndpoints{
				Locality: &core.Locality{
					Zone: instance.AvailabilityZone,
				},
			}
			localityEpMap[locality] = locLbEps
		}
		log.Infoa("EDS LOCALITY ep: ", (*lbEp).String(), localityEpMap)
		locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, *lbEp)
	}
	log.Infoa("EDS LOCALITY: ", localityEpMap)
	out := make([]endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		out = append(out, *locLbEps)
		log.Infoa("EDS LOCALITY add: ", locLbEps.String())
	}
	return out
}
