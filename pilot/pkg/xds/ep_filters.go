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

package xds

import (
	"math"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/network"
)

// EndpointsByNetworkFilter is a network filter function to support Split Horizon EDS - filter the endpoints based on the network
// of the connected sidecar. The filter will filter out all endpoints which are not present within the
// sidecar network and add a gateway endpoint to remote networks that have endpoints
// (if gateway exists and its IP is an IP and not a dns name).
// Information for the mesh networks is provided as a MeshNetwork config map.
func (b *EndpointBuilder) EndpointsByNetworkFilter(endpoints []*LocLbEndpointsAndOptions) []*LocLbEndpointsAndOptions {
	if !b.push.NetworkManager().IsMultiNetworkEnabled() {
		// Multi-network is not configured (this is the case by default). Just access all endpoints directly.
		return endpoints
	}

	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := make([]*LocLbEndpointsAndOptions, 0)

	// Scale all weights by the maximum number of gateways that might appear for a network.
	// This will allow us to more easily spread traffic to the endpoint across multiple
	// network gateways, increasing reliability of the endpoint.
	scaleFactor := b.push.NetworkManager().GetMaxGatewaysPerNetwork()

	// Go through all cluster endpoints and add those with the same network as the sidecar
	// to the result. Also count the number of endpoints per each remote network while
	// iterating so that it can be used as the weight for the gateway endpoint
	for _, ep := range endpoints {
		lbEndpoints := &LocLbEndpointsAndOptions{
			llbEndpoints: endpoint.LocalityLbEndpoints{
				Locality: ep.llbEndpoints.Locality,
				Priority: ep.llbEndpoints.Priority,
				// Endpoints and weight will be reset below.
			},
		}

		// Create a map to keep track of the gateways used and their aggregate weights.
		gatewayWeights := make(map[model.NetworkGateway]uint32)

		// Process all of the endpoints.
		for i, lbEp := range ep.llbEndpoints.LbEndpoints {
			// Copy the endpoint in order to expand the load balancing weight.
			// When multiplying, be careful to avoid overflow - clipping the
			// result at the maximum value for uint32.
			weight := b.scaleEndpointLBWeight(lbEp, scaleFactor)
			lbEp := proto.Clone(lbEp).(*endpoint.LbEndpoint)
			lbEp.LoadBalancingWeight = &wrappers.UInt32Value{
				Value: weight,
			}

			istioEndpoint := ep.istioEndpoints[i]
			epNetwork := istioEndpoint.Network
			epCluster := istioEndpoint.Locality.ClusterID
			gateways := b.selectNetworkGateways(epNetwork, epCluster)

			// Check if the endpoint is directly reachable. It's considered directly reachable if
			// the endpoint is either on the local network or on a remote network that can be reached
			// directly from the local network.
			if b.proxy.InNetwork(epNetwork) || len(gateways) == 0 {
				// The endpoint is directly reachable - just add it.
				lbEndpoints.emplace(lbEp, ep.tunnelMetadata[i])
				continue
			}

			// If the proxy can't view the network for this endpoint, exclude it entirely.
			if !b.canViewNetwork(epNetwork) {
				continue
			}

			// Cross-network traffic relies on mTLS to be enabled for SNI routing
			// TODO BTS may allow us to work around this
			if b.mtlsChecker.isMtlsDisabled(lbEp) {
				continue
			}

			// Apply the weight for this endpoint to the network gateways.
			remainingWeight := weight
			for remainingWeight > 0 {
				// Spread the remaining weight across the gateways.
				weightPerGateway := remainingWeight / uint32(len(gateways))
				if weightPerGateway == 0 {
					// There are more gateways than weight. Just apply 1 to each gateway until all the
					// weight has been exhausted.
					weightPerGateway = 1
				}

				for _, gateway := range gateways {
					// Add the portion of weight to this gateway.
					if weightPerGateway > remainingWeight {
						weightPerGateway = remainingWeight
					}
					gatewayWeights[*gateway] += weightPerGateway

					// Update the remaining weight.
					remainingWeight -= weightPerGateway
					if remainingWeight == 0 {
						// The weight for this endpoint has been exhausted. We're done.
						break
					}
				}
			}
		}

		// Now create endpoints for the gateways.
		for gw, weight := range gatewayWeights {
			epAddr := util.BuildAddress(gw.Addr, gw.Port)

			// Generate a fake IstioEndpoint to carry network and cluster information.
			gwIstioEp := &model.IstioEndpoint{
				Network: gw.Network,
				Locality: model.Locality{
					ClusterID: gw.Cluster,
				},
			}

			// Generate the EDS endpoint for this gateway.
			gwEp := &endpoint.LbEndpoint{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: epAddr,
					},
				},
				LoadBalancingWeight: &wrappers.UInt32Value{
					Value: weight,
				},
			}
			// TODO: figure out a way to extract locality data from the gateway public endpoints in meshNetworks
			gwEp.Metadata = util.BuildLbEndpointMetadata(gw.Network, model.IstioMutualTLSModeLabel,
				"", "", b.clusterID, labels.Instance{})
			// Currently gateway endpoint does not support tunnel.
			lbEndpoints.append(gwIstioEp, gwEp, networking.MakeTunnelAbility())
		}

		// Endpoint members could be stripped or aggregated by network. Adjust weight value here.
		lbEndpoints.refreshWeight()
		filtered = append(filtered, lbEndpoints)
	}

	return filtered
}

// selectNetworkGateways chooses the gateways that best match the network and cluster. If there is
// no match for the network+cluster, then all gateways matching the network are returned. Preferring
// gateways that match against cluster has the following advantages:
//
//   1. Potentially reducing extra latency incurred when the gateway and endpoint reside in different
//      clusters.
//
//   2. Enables Kubernetes MCS use cases, where endpoints for a service might be exported in one
//      cluster but not another within the same network. By targeting the gateway for the cluster
//      where the exported endpoints reside, we ensure that we only send traffic to exported endpoints.
func (b *EndpointBuilder) selectNetworkGateways(nw network.ID, c cluster.ID) []*model.NetworkGateway {
	// Get the gateways for this network+cluster combination.
	gws := b.push.NetworkManager().GatewaysForNetworkAndCluster(nw, c)
	if len(gws) == 0 {
		// No match for network+cluster, just match the network.
		gws = b.push.NetworkManager().GatewaysForNetwork(nw)
	}
	return gws
}

func (b *EndpointBuilder) scaleEndpointLBWeight(ep *endpoint.LbEndpoint, scaleFactor uint32) uint32 {
	weight := uint32(math.MaxUint32)
	if ep.GetLoadBalancingWeight().Value < math.MaxUint32/scaleFactor {
		weight = ep.GetLoadBalancingWeight().Value * scaleFactor
	}
	return weight
}

func (b *EndpointBuilder) gatewaysByNetwork() map[network.ID][]*model.NetworkGateway {
	gateways := b.push.NetworkManager().AllGateways()
	byNetwork := make(map[network.ID][]*model.NetworkGateway)
	for _, gateway := range gateways {
		networkGateways := append(byNetwork[gateway.Network], gateway)
		byNetwork[gateway.Network] = networkGateways
	}
	return byNetwork
}

// EndpointsWithMTLSFilter removes all endpoints that do not handle mTLS. This is determined by looking at
// auto-mTLS, DestinationRule, and PeerAuthentication to determine if we would send mTLS to these endpoints.
// Note there is no guarantee these destinations *actually* handle mTLS; just that we are configured to send mTLS to them.
func (b *EndpointBuilder) EndpointsWithMTLSFilter(endpoints []*LocLbEndpointsAndOptions) []*LocLbEndpointsAndOptions {
	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := make([]*LocLbEndpointsAndOptions, 0)

	// Go through all cluster endpoints and add those with mTLS enabled
	for _, ep := range endpoints {
		lbEndpoints := &LocLbEndpointsAndOptions{
			llbEndpoints: endpoint.LocalityLbEndpoints{
				Locality: ep.llbEndpoints.Locality,
				Priority: ep.llbEndpoints.Priority,
				// Endpoints and will be reset below.
			},
		}

		for i, lbEp := range ep.llbEndpoints.LbEndpoints {
			if b.mtlsChecker.isMtlsDisabled(lbEp) {
				// no mTLS, skip it
				continue
			}
			lbEndpoints.emplace(lbEp, ep.tunnelMetadata[i])
		}

		filtered = append(filtered, lbEndpoints)
	}

	return filtered
}

func envoytransportSocketMetadata(ep *endpoint.LbEndpoint, key string) string {
	if ep.Metadata != nil &&
		ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey] != nil &&
		ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].Fields != nil &&
		ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].Fields[key] != nil {
		return ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].Fields[key].GetStringValue()
	}
	return ""
}
