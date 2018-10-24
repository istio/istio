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
	"net"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"
	
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// EndpointsFilterFunc is a function that filters data from the ClusterLoadAssignment and returns updated one
type EndpointsFilterFunc func(endpoints []endpoint.LocalityLbEndpoints, conn *XdsConnection, env *model.Environment) []endpoint.LocalityLbEndpoints

// EndpointsByNetworkFilter is a network filter function to support Split Horizon EDS - filter the endpoints based on the network
// of the connected sidecar. The filter will filter out all endpoints which are not present within the
// sidecar network and add a gateway endpoint to remote networks that have endpoints (if gateway exists).
// Information for the mesh networks is provided as a MeshNetwork config map.
func EndpointsByNetworkFilter(endpoints []endpoint.LocalityLbEndpoints, conn *XdsConnection, env *model.Environment) []endpoint.LocalityLbEndpoints {
	// If the sidecar does not specify a network, ignore Split Horizon EDS and return all
	network, found := conn.modelNode.Metadata["ISTIO_NETWORK"]
	if !found {

		// TODO: try to get the network by querying the service registry to get the
		// network from its endpoint

		// Couldn't find the sidecar network
		return endpoints
	}

	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := []endpoint.LocalityLbEndpoints{}

	// Weight (number of endpoints) for the EDS cluster for each remote networks
	remoteEps := map[string]uint32{}

	// Go through all cluster endpoints and add those with the same network as the sidecar
	// to the result. Also count the number of endpoints per each remote network while
	// iterating so that it can be used as the weight for the gateway endpoint
	for _, ep := range endpoints {
		onlyLocalLbEndpoints := []endpoint.LbEndpoint{}
		foundRemote := false
		for _, lbEp := range ep.LbEndpoints {
			epNetwork := ""
			if lbEp.Metadata != nil && lbEp.Metadata.FilterMetadata["istio"] != nil &&
				lbEp.Metadata.FilterMetadata["istio"].Fields != nil &&
				lbEp.Metadata.FilterMetadata["istio"].Fields["network"] != nil {
				epNetwork = lbEp.Metadata.FilterMetadata["istio"].Fields["network"].GetStringValue()
			}
			if epNetwork == network {
				// This is a local endpoint
				onlyLocalLbEndpoints = append(onlyLocalLbEndpoints, lbEp)
			} else {
				// Remote endpoint. Increase the weight counter
				remoteEps[epNetwork]++
				foundRemote = true
			}
		}

		if !foundRemote {
			// This LocalityLbEndpoints has no remote endpoints so just
			// add it to the result
			filtered = append(filtered, ep)
		} else {
			// This LocalityLbEndpoints has remote endpoint so add to the result
			// a new one that holds only local endpoints
			newEp := endpoint.LocalityLbEndpoints{
				Locality:            ep.Locality,
				LbEndpoints:         onlyLocalLbEndpoints,
				LoadBalancingWeight: ep.LoadBalancingWeight,
				Priority:            ep.Priority,
			}
			filtered = append(filtered, newEp)
		}
	}

	// If there is no MeshNetworks configuration, we don't have gateways information
	// so just return the filtered results (with no remote endpoints)
	if env.MeshNetworks == nil {
		return filtered
	}

	// Iterate over all networks that have the cluster endpoint (weight>0) and
	// for each one of those add a new endpoint that points to the network's
	// gateway with the relevant weight
	for n, w := range remoteEps {
		networkConf, found := env.MeshNetworks.Networks[n]
		if !found {
			continue
		}
		gws := networkConf.Gateways
		if len(gws) == 0 {
			continue
		}

		// There may be multiple gateways for the network. Add an LbEndpoint for
		// each one of them
		gwEndpoints := []endpoint.LbEndpoint{}
		for _, gw := range gws {
			// If the gateway address is DNS we can't add it to the endpoints
			if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
				addr := util.BuildAddress(gw.GetAddress(), gw.Port)
				gwEp := endpoint.LbEndpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &addr,
					},
				}
				gwEndpoints = append(gwEndpoints, gwEp)
			}

			if gw.GetRegistryServiceName() != "" {
				//TODO add support for getting the gateway address from the service registry
			}
		}

		// No endpoints for this gatway, don't add it
		if len(gwEndpoints) == 0 {
			continue
		}

		// Create the gateway endpoint and add it to the CLA
		gwLocEp := endpoint.LocalityLbEndpoints{
			LbEndpoints: gwEndpoints,
			LoadBalancingWeight: &types.UInt32Value{
				Value: uint32(w),
			},
		}
		filtered = append(filtered, gwLocEp)
	}

	return filtered
}
