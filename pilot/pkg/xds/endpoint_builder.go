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
	"sort"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Return the tunnel type for this endpoint builder. If the endpoint builder builds h2tunnel, the final endpoint
// collection includes only the endpoints which support H2 tunnel.
func GetTunnelBuilderType(clusterName string, proxy *model.Proxy, push *model.PushContext) string {
	return networking.NoTunnelTypeName
}

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName     string
	network         string
	networkView     map[string]bool
	clusterID       string
	locality        *core.Locality
	destinationRule *config.Config
	service         *model.Service
	tunnelType      string

	// These fields are provided for convenience only
	subsetName string
	hostname   host.Name
	port       int
	push       *model.PushContext
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	_, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	svc := push.ServiceForHostname(proxy, hostname)
	return EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		networkView:     model.GetNetworkView(proxy),
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		service:         svc,
		destinationRule: push.DestinationRule(proxy, svc),
		tunnelType:      GetTunnelBuilderType(clusterName, proxy, push),

		push:       push,
		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
	}
}

func (b EndpointBuilder) DestinationRule() *networkingapi.DestinationRule {
	if b.destinationRule == nil {
		return nil
	}
	return b.destinationRule.Spec.(*networkingapi.DestinationRule)
}

// Key provides the eds cache key and should include any information that could change the way endpoints are generated.
func (b EndpointBuilder) Key() string {
	params := []string{b.clusterName, b.network, b.clusterID, util.LocalityToString(b.locality), b.tunnelType}
	if b.destinationRule != nil {
		params = append(params, b.destinationRule.Name+"/"+b.destinationRule.Namespace)
	}
	if b.service != nil {
		params = append(params, string(b.service.Hostname)+"/"+b.service.Attributes.Namespace)
	}
	if b.networkView != nil {
		nv := make([]string, 0, len(b.networkView))
		for nw := range b.networkView {
			nv = append(nv, nw)
		}
		sort.Strings(nv)
		params = append(params, nv...)
	}
	return strings.Join(params, "~")
}

// MultiNetworkConfigured determines if we have gateways to use for building cross-network endpoints.
func (b *EndpointBuilder) MultiNetworkConfigured() bool {
	return b.push.NetworkGateways() != nil && len(b.push.NetworkGateways()) > 0
}

func (b EndpointBuilder) Cacheable() bool {
	// If service is not defined, we cannot do any caching as we will not have a way to
	// invalidate the results.
	// Service being nil means the EDS will be empty anyways, so not much lost here.
	return b.service != nil
}

func (b EndpointBuilder) DependentConfigs() []model.ConfigKey {
	configs := []model.ConfigKey{}
	if b.destinationRule != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.DestinationRule, Name: b.destinationRule.Name, Namespace: b.destinationRule.Namespace})
	}
	if b.service != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.ServiceEntry, Name: string(b.service.Hostname), Namespace: b.service.Attributes.Namespace})
	}
	return configs
}

func (b *EndpointBuilder) canViewNetwork(network string) bool {
	if b.networkView == nil {
		return true
	}
	return b.networkView[network]
}

// TODO(lambdai): add
type EndpointTunnelMetadata int

type LocLbEndpointsAndOptions struct {
	// The protobuf message which contains LbEndpoint slice.
	llbEndpoints   endpoint.LocalityLbEndpoints
	// The runtime information of the LbEndpoint slice. Each LbEndpoint has individual metadata at the same index.
	tunnelMetadata []EndpointTunnelMetadata
}

func makeTunnelMetadata (le *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) EndpointTunnelMetadata {
	return EndpointTunnelMetadata(0)
}

func (e *LocLbEndpointsAndOptions) append(le *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) {
	e.llbEndpoints.LbEndpoints = append(e.llbEndpoints.LbEndpoints, le)
	e.tunnelMetadata = append(e.tunnelMetadata, makeTunnelMetadata(le, tunnelOpt))
}

func (e *LocLbEndpointsAndOptions) emplace(le *endpoint.LbEndpoint, tunnelMetadata EndpointTunnelMetadata) {
	e.llbEndpoints.LbEndpoints = append(e.llbEndpoints.LbEndpoints, le)
	e.tunnelMetadata = append(e.tunnelMetadata, tunnelMetadata)
}

func (e *LocLbEndpointsAndOptions) refreshWeight() {
	var weight *wrappers.UInt32Value
	if len(e.llbEndpoints.LbEndpoints) == 0 {
		weight = nil
	} else {
		weight = &wrappers.UInt32Value{}
		for _, lbEp := range e.llbEndpoints.LbEndpoints {
			weight.Value += lbEp.GetLoadBalancingWeight().Value
		}
	}
	e.llbEndpoints.LoadBalancingWeight = weight
	e.checkInvariance()
}

func (e *LocLbEndpointsAndOptions) checkInvariance() {
	if len(e.llbEndpoints.LbEndpoints) != len(e.tunnelMetadata) {
		panic(" len(e.llbEndpoints.LbEndpoints) != len(e.tunnelMetadata)")
	}
}
// build LocalityLbEndpoints for a cluster from existing EndpointShards.
func (b *EndpointBuilder) buildLocalityLbEndpointsFromShards(
	shards *EndpointShards,
	svcPort *model.Port,
) []*LocLbEndpointsAndOptions {
	localityEpMap := make(map[string]*LocLbEndpointsAndOptions)

	// get the subset labels
	epLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.push.IsClusterLocal(b.service)

	shards.mutex.Lock()
	// The shards are updated independently, now need to filter and merge
	// for this cluster
	for clusterID, endpoints := range shards.Shards {
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (clusterID != b.clusterID) {
			continue
		}

		for _, ep := range endpoints {
			if svcPort.Name != ep.ServicePortName {
				continue
			}
			// Port labels
			if !epLabels.HasSubsetOf(ep.Labels) {
				continue
			}

			locLbEps, found := localityEpMap[ep.Locality.Label]
			if !found {
				locLbEps = &LocLbEndpointsAndOptions{
					endpoint.LocalityLbEndpoints{
						Locality:    util.ConvertLocality(ep.Locality.Label),
						LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(endpoints)),
					},
					make([]EndpointTunnelMetadata, 0, len(endpoints)),
				}
				localityEpMap[ep.Locality.Label] = locLbEps
			}
			if ep.EnvoyEndpoint == nil {
				ep.EnvoyEndpoint = buildEnvoyLbEndpoint(ep)
			}
			locLbEps.append(ep.EnvoyEndpoint, ep.TunnelAbility)
		}
	}
	shards.mutex.Unlock()

	locEps := make([]*LocLbEndpointsAndOptions, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		var weight uint32
		for _, ep := range locLbEps.llbEndpoints.LbEndpoints {
			weight += ep.LoadBalancingWeight.GetValue()
		}
		locLbEps.llbEndpoints.LoadBalancingWeight = &wrappers.UInt32Value{
			Value: weight,
		}
		locEps = append(locEps, locLbEps)
	}

	if len(locEps) == 0 {
		b.push.AddMetric(model.ProxyStatusClusterNoInstances, b.clusterName, "", "")
	}

	return locEps
}

// TODO(lambdai): For h2tunnel: mutate endpoint port to 15009 if the endpoint supports h2 tunnel.
func (b *EndpointBuilder) ApplyTunnelSetting(endpoints *endpoint.ClusterLoadAssignment) *endpoint.ClusterLoadAssignment {
	return endpoints
}

// buildEnvoyLbEndpoint packs the endpoint based on istio info.
func buildEnvoyLbEndpoint(e *model.IstioEndpoint) *endpoint.LbEndpoint {
	addr := util.BuildAddress(e.Address, e.EndpointPort)

	epWeight := e.LbWeight
	if epWeight == 0 {
		epWeight = 1
	}
	ep := &endpoint.LbEndpoint{
		LoadBalancingWeight: &wrappers.UInt32Value{
			Value: epWeight,
		},
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: addr,
			},
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not removepilot/pkg/xds/fake.go
	ep.Metadata = util.BuildLbEndpointMetadata(e.Network, e.TLSMode, e.WorkloadName, e.Namespace, e.Labels)

	return ep
}
