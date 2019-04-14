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
	network, found := conn.modelNode.Metadata[model.NodeMetadataNetwork]
	if !found {
		// Couldn't find the sidecar network, using default/local
		network = ""
	}

	// calculate the multiples of weight.
	// It is needed to normalize the LB Weight across different networks.
	multiples := 1
	for _, network := range env.MeshNetworks.Networks {
		num := 0
		for _, gw := range network.Gateways {
			if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
				num++
			}
		}
		if num > 1 {
			multiples *= num
		}
	}

	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := []endpoint.LocalityLbEndpoints{}

	// Go through all cluster endpoints and add those with the same network as the sidecar
	// to the result. Also count the number of endpoints per each remote network while
	// iterating so that it can be used as the weight for the gateway endpoint
	for _, ep := range endpoints {
		// Weight (number of endpoints) for the EDS cluster for each remote networks
		remoteEps := map[string]uint32{}

		lbEndpoints := []endpoint.LbEndpoint{}
		for _, lbEp := range ep.LbEndpoints {
			epNetwork := istioMetadata(lbEp, "network")
			if epNetwork == network {
				// This is a local endpoint
				lbEp.LoadBalancingWeight = &types.UInt32Value{
					Value: uint32(multiples),
				}
				lbEndpoints = append(lbEndpoints, lbEp)
			} else {
				// Remote endpoint. Increase the weight counter
				remoteEps[epNetwork]++
			}
		}

		// Add endpoints to remote networks' gateways

		// Iterate over all networks that have the cluster endpoint (weight>0) and
		// for each one of those add a new endpoint that points to the network's
		// gateway with the relevant weight
		for n, w := range remoteEps {
			networkConf, found := env.MeshNetworks.Networks[n]
			if !found {
				adsLog.Infof("the endpoints within network %s will be ignored for no network configured", n)
				continue
			}
			gws := networkConf.Gateways
			if len(gws) == 0 {
				adsLog.Infof("the endpoints within network %s will be ignored for no gateways configured", n)
				continue
			}

			gwEps := []endpoint.LbEndpoint{}
			// There may be multiples gateways for the network. Add an LbEndpoint for
			// each one of them
			for _, gw := range gws {
				var gwEp *endpoint.LbEndpoint
				//TODO add support for getting the gateway address from the service registry

				// If the gateway address in the config was a hostname it got already resolved
				// and replaced with an IP address when loading the config
				if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
					addr := util.BuildAddress(gw.GetAddress(), gw.Port)
					gwEp = &endpoint.LbEndpoint{
						HostIdentifier: &endpoint.LbEndpoint_Endpoint{
							Endpoint: &endpoint.Endpoint{
								Address: &addr,
							},
						},
						LoadBalancingWeight: &types.UInt32Value{
							Value: w,
						},
					}
				}

				if gwEp != nil {
					gwEps = append(gwEps, *gwEp)
				}
			}
			weight := w * uint32(multiples/len(gwEps))
			for _, gwEp := range gwEps {
				gwEp.LoadBalancingWeight.Value = weight
				lbEndpoints = append(lbEndpoints, gwEp)
			}
		}

		// Found local endpoint(s) so add to the result a new one LocalityLbEndpoints
		// that holds only the local endpoints
		newEp := createLocalityLbEndpoints(&ep, lbEndpoints)
		filtered = append(filtered, *newEp)
	}

	return filtered
}

// TODO: remove this, filtering should be done before generating the config, and
// network metadata should not be included in output. A node only receives endpoints
// in the same network as itself - so passing an network meta, with exactly
// same value that the node itself had, on each endpoint is a bit absurd.

// Checks whether there is an istio metadata string value for the provided key
// within the endpoint metadata. If exists, it will return the value.
func istioMetadata(ep endpoint.LbEndpoint, key string) string {
	if ep.Metadata != nil &&
		ep.Metadata.FilterMetadata["istio"] != nil &&
		ep.Metadata.FilterMetadata["istio"].Fields != nil &&
		ep.Metadata.FilterMetadata["istio"].Fields[key] != nil {
		return ep.Metadata.FilterMetadata["istio"].Fields[key].GetStringValue()
	}
	return ""
}

func createLocalityLbEndpoints(base *endpoint.LocalityLbEndpoints, lbEndpoints []endpoint.LbEndpoint) *endpoint.LocalityLbEndpoints {
	var weight *types.UInt32Value
	if len(lbEndpoints) == 0 {
		weight = nil
	} else {
		weight = &types.UInt32Value{}
		for _, lbEp := range lbEndpoints {
			weight.Value += lbEp.GetLoadBalancingWeight().Value
		}
	}
	ep := &endpoint.LocalityLbEndpoints{
		Locality:            base.Locality,
		LbEndpoints:         lbEndpoints,
		LoadBalancingWeight: weight,
		Priority:            base.Priority,
	}
	return ep
}

// LoadBalancingWeightNormalize set LoadBalancingWeight with a valid value.
func LoadBalancingWeightNormalize(endpoints []endpoint.LocalityLbEndpoints) []endpoint.LocalityLbEndpoints {
	return util.LocalityLbWeightNormalize(endpoints)
}
