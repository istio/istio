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

package endpoints

import (
	"math"
	"net"
	"strconv"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/util/protomarshal"
)

// innerConnectOriginate is the name for the resources associated with establishing double-HBONE connection.
// Duplicated from networking/core/waypoint.go to avoid import cycle
const innerConnectOriginate = "inner_connect_originate"

// EndpointsByNetworkFilter is a network filter function to support Split Horizon EDS - filter the endpoints based on the network
// of the connected sidecar. The filter will filter out all endpoints which are not present within the
// sidecar network and add a gateway endpoint to remote networks that have endpoints
// (if gateway exists and its IP is an IP and not a dns name).
// Information for the mesh networks is provided as a MeshNetwork config map.
func (b *EndpointBuilder) EndpointsByNetworkFilter(endpoints []*LocalityEndpoints) []*LocalityEndpoints {
	// In sidecar mode multi-network setup, when we have multiple networks but no E/W gateways configured we still
	// generate EDS endpoints for remote networks as if they were on the same network. In practice it may not
	// actually work, e.g., when pods are not directly reachable without E/W gateways, so in that case EDS
	// endpoints for remote networks will be broken essentially.
	//
	// That does not seem like the most intuitive failure mode TBH, but because this logic has been around for
	// years, we don't want to change it because it might actually break existing set up. For ambient multi-network
	// though we can in principle go a different way and not generate EDS endpoints for remote networks when there
	// are no gateways.
	//
	// It's debatable whether we should diverge here from sidecar or not - there are arguments on both sides:
	//  - on the one hand, chosing a different behavior here might be surprising for folks migrating from sidecar
	//  - on the other hand, if we preserve the current behavior it would be inconsistent with how ztunnel handles
	//    the same situation.
	//
	// After discussing it in the WG meeting, the preference was to change the behavior in ambient mode to not
	// generate EDS endpoints for remote networks, so that's why the logic between ambient and sidecar mode here
	// is different.
	isAmbientWaypoint := features.EnableAmbientMultiNetwork && isWaypointProxy(b.proxy)

	if !b.gateways().IsMultiNetworkEnabled() && !isAmbientWaypoint {
		// Multi-network is not configured (this is the case by default). Just access all endpoints directly.
		return endpoints
	}

	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := make([]*LocalityEndpoints, 0)

	// Scale all weights by the lcm of gateways per network and gateways per cluster.
	// This will allow us to more easily spread traffic to the endpoint across multiple
	// network gateways, increasing reliability of the endpoint.
	scaleFactor := b.gateways().GetLBWeightScaleFactor()
	if scaleFactor == 0 {
		// If there are no E/W gateways, it's fine we just don't need to scale anything
		scaleFactor = 1
	}

	// Go through all cluster endpoints and add those with the same network as the sidecar
	// to the result. Also count the number of endpoints per each remote network while
	// iterating so that it can be used as the weight for the gateway endpoint
	for _, ep := range endpoints {
		lbEndpoints := &LocalityEndpoints{
			llbEndpoints: endpoint.LocalityLbEndpoints{
				Locality: ep.llbEndpoints.Locality,
				Priority: ep.llbEndpoints.Priority,
				// Endpoints and weight will be reset below.
			},
		}

		// Create a map to keep track of the gateways used and their aggregate weights.
		gatewayWeights := make(map[model.NetworkGateway]uint32)

		// Process all the endpoints.
		for i, lbEp := range ep.llbEndpoints.LbEndpoints {
			istioEndpoint := ep.istioEndpoints[i]

			// If the proxy can't view the network for this endpoint, exclude it entirely.
			if !b.proxyView.IsVisible(istioEndpoint) {
				continue
			}

			epNetwork := istioEndpoint.Network
			epCluster := istioEndpoint.Locality.ClusterID
			gateways := b.selectNetworkGateways(epNetwork, epCluster)

			// We are generating endpoints for an ambient waypoint and we encountered an endpoint on a remote network.
			// Check if we allow waypoints to talk across networks (EnableAmbientWaypointMultiNetwork feature flag)
			// and whether we have an E/W gateway we can use. If neither is true, then just ignore the endpoint
			// completely.
			if isAmbientWaypoint && !b.proxy.InNetwork(epNetwork) {
				if !features.EnableAmbientWaypointMultiNetwork {
					continue
				}
				if len(gateways) == 0 {
					// We have an endpoint on a remote network, but no E/W gateway configured? Seems like a
					// misconfiguration, let's log it for visibility
					log.Warnf("Workload %s belongs to a different network (%s), but no E/W gateway configured, skipping it.", istioEndpoint.WorkloadName, epNetwork)
					continue
				}
			}

			// Copy the endpoint in order to expand the load balancing weight.
			// When multiplying, be careful to avoid overflow - clipping the
			// result at the maximum value for uint32.
			weight := b.scaleEndpointLBWeight(lbEp, scaleFactor)
			if lbEp.GetLoadBalancingWeight().GetValue() != weight {
				lbEp = protomarshal.Clone(lbEp)
				lbEp.LoadBalancingWeight = &wrappers.UInt32Value{
					Value: weight,
				}
			}

			// Check if the endpoint is directly reachable. It's considered directly reachable if
			// the endpoint is either on the local network or on a remote network that can be reached
			// directly from the local network.
			if b.proxy.InNetwork(epNetwork) || len(gateways) == 0 {
				// The endpoint is directly reachable - just add it.
				// If there is no gateway, the address must not be empty
				if util.GetEndpointHost(lbEp) != "" {
					lbEndpoints.append(ep.istioEndpoints[i], lbEp)
				}

				continue
			}

			// Cross-network traffic relies on double-HBONE in ambient mode and on mTLS for SNI routing in sidecar mode.
			// So if we are not in ambient multi-network mode and mTLS is not enabled for the target endpoint on a remote
			// network we skip it altogether.
			// TODO BTS may allow us to work around this
			if !isAmbientWaypoint && !isMtlsEnabled(lbEp) {
				continue
			}

			// Apply the weight for this endpoint to the network gateways.
			splitWeightAmongGateways(weight, gateways, gatewayWeights)
		}

		// Sort the gateways into an ordered list so that the generated endpoints are deterministic.
		gateways := maps.Keys(gatewayWeights)
		gateways = model.SortGateways(gateways)

		// Create endpoints for the gateways.
		for _, gw := range gateways {
			epWeight := gatewayWeights[gw]
			if epWeight == 0 {
				log.Warnf("gateway weight must be greater than 0, scaleFactor is %d", scaleFactor)
				epWeight = 1
			}
			// Generate a fake IstioEndpoint to carry network and cluster information.
			gwIstioEp := &model.IstioEndpoint{
				Network: gw.Network,
				Locality: model.Locality{
					ClusterID: gw.Cluster,
				},
				Labels: labelutil.AugmentLabels(nil, gw.Cluster, "", "", gw.Network),
			}

			// Here we generate the EDS endpoint for the E/W gateways that replace the original endpoint.
			// Depending on whether we operate in ambient mode or not, we generated endpoints for E/W
			// gateways differently as we use somewhat different protocols in those two distinct cases.
			var gwEp *endpoint.LbEndpoint

			if isAmbientWaypoint {
				gwAddr := gw.Addr
				gwPort := int(gw.HBONEPort)

				addr := net.JoinHostPort(gwAddr, strconv.Itoa(gwPort))
				svcPort := b.servicePort(b.port)

				gwEp = &endpoint.LbEndpoint{
					HostIdentifier: &endpoint.LbEndpoint_Endpoint{
						Endpoint: &endpoint.Endpoint{
							// We need to redirect to an internal listener that will tunnel the data through
							// a double-HBONE, we still use the E/W gateway address though for the endpoint
							// id.
							Address: util.BuildInternalAddressWithIdentifier(innerConnectOriginate, addr),
						},
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: epWeight,
					},
					Metadata: &core.Metadata{},
				}

				// TODO: figure out a way to extract locality data from the gateway public endpoints in meshNetworks
				util.AppendLbEndpointMetadata(&model.EndpointMetadata{
					Network: gw.Network,
					// I don't think that TLSMode affects anythig downstream of this code anymore, but for ambient
					// mode we do not rely on the legacy Istio mTLS, so I explicitly mark it as disabled.
					TLSMode:   model.DisabledTLSModeLabel,
					ClusterID: gw.Cluster,
					Labels:    labels.Instance{},
				}, gwEp.Metadata)

				// We need to add original dst metadata key with the actual E/W gateway address that we will connect to
				gwEp.Metadata.FilterMetadata[util.OriginalDstMetadataKey] = util.BuildTunnelMetadataStruct(gwAddr, gwPort, "")
				// and we need the original service domain name and port that to put in the :authority of the HTTP2 CONNECT.
				util.AppendDoubleHBONEMetadata(string(b.service.Hostname), svcPort.Port, gwEp.Metadata)
				if b.dir != model.TrafficDirectionInboundVIP {
					gwEp.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey] = &structpb.Struct{
						Fields: map[string]*structpb.Value{
							model.TunnelLabelShortName: {Kind: &structpb.Value_StringValue{StringValue: model.TunnelHTTP}},
						},
					}
				}
			} else {
				epAddr := util.BuildAddress(gw.Addr, gw.Port)
				gwEp = &endpoint.LbEndpoint{
					HostIdentifier: &endpoint.LbEndpoint_Endpoint{
						Endpoint: &endpoint.Endpoint{
							Address: epAddr,
						},
					},
					LoadBalancingWeight: &wrappers.UInt32Value{
						Value: epWeight,
					},
					Metadata: &core.Metadata{},
				}

				// TODO: figure out a way to extract locality data from the gateway public endpoints in meshNetworks
				util.AppendLbEndpointMetadata(&model.EndpointMetadata{
					Network:   gw.Network,
					TLSMode:   model.IstioMutualTLSModeLabel,
					ClusterID: gw.Cluster,
					Labels:    labels.Instance{},
				}, gwEp.Metadata)
			}

			// Currently gateway endpoint does not support tunnel.
			lbEndpoints.append(gwIstioEp, gwEp)
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
//  1. Potentially reducing extra latency incurred when the gateway and endpoint reside in different
//     clusters.
//
//  2. Enables Kubernetes MCS use cases, where endpoints for a service might be exported in one
//     cluster but not another within the same network. By targeting the gateway for the cluster
//     where the exported endpoints reside, we ensure that we only send traffic to exported endpoints.
func (b *EndpointBuilder) selectNetworkGateways(nw network.ID, c cluster.ID) []model.NetworkGateway {
	// Get the gateways for this network+cluster combination.
	gws := b.gateways().GatewaysForNetworkAndCluster(nw, c)
	if len(gws) == 0 {
		// No match for network+cluster, just match the network.
		gws = b.gateways().GatewaysForNetwork(nw)
	}

	// If we operate in ambient multi-network mode skip gateways that don't have HBONE port
	if features.EnableAmbientMultiNetwork && isWaypointProxy(b.proxy) {
		var ambientGws []model.NetworkGateway
		for _, gw := range gws {
			if gw.HBONEPort == 0 {
				continue
			}
			ambientGws = append(ambientGws, gw)
		}
		return ambientGws
	}

	return gws
}

func (b *EndpointBuilder) scaleEndpointLBWeight(ep *endpoint.LbEndpoint, scaleFactor uint32) uint32 {
	if ep.GetLoadBalancingWeight() == nil || ep.GetLoadBalancingWeight().Value == 0 {
		return scaleFactor
	}
	weight := uint32(math.MaxUint32)
	if ep.GetLoadBalancingWeight().Value < math.MaxUint32/scaleFactor {
		weight = ep.GetLoadBalancingWeight().Value * scaleFactor
	}
	return weight
}

// Apply the weight for this endpoint to the network gateways.
func splitWeightAmongGateways(weight uint32, gateways []model.NetworkGateway, gatewayWeights map[model.NetworkGateway]uint32) {
	// Spread the weight across the gateways.
	weightPerGateway := weight / uint32(len(gateways))
	for _, gateway := range gateways {
		gatewayWeights[gateway] += weightPerGateway
	}
}

// EndpointsWithMTLSFilter removes all endpoints that do not handle mTLS. This is determined by looking at
// auto-mTLS, DestinationRule, and PeerAuthentication to determine if we would send mTLS to these endpoints.
// Note there is no guarantee these destinations *actually* handle mTLS; just that we are configured to send mTLS to them.
func (b *EndpointBuilder) EndpointsWithMTLSFilter(endpoints []*LocalityEndpoints) []*LocalityEndpoints {
	// A new array of endpoints to be returned that will have both local and
	// remote gateways (if any)
	filtered := make([]*LocalityEndpoints, 0)

	// Go through all cluster endpoints and add those with mTLS enabled
	for _, ep := range endpoints {
		lbEndpoints := &LocalityEndpoints{
			llbEndpoints: endpoint.LocalityLbEndpoints{
				Locality: ep.llbEndpoints.Locality,
				Priority: ep.llbEndpoints.Priority,
				// Endpoints and will be reset below.
			},
		}

		for i, lbEp := range ep.llbEndpoints.LbEndpoints {
			if !isMtlsEnabled(lbEp) {
				// no mTLS, skip it
				continue
			}
			lbEndpoints.append(ep.istioEndpoints[i], lbEp)
		}

		lbEndpoints.refreshWeight()
		filtered = append(filtered, lbEndpoints)
	}

	return filtered
}
