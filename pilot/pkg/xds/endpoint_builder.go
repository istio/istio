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
	"github.com/golang/protobuf/proto"
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
// collection includes only the endpoints which support H2 tunnel and the non-tunnel endpoints. The latter case is to
// support multi-cluster service.
// Revisit non-tunnel endpoint decision once the gateways supports tunnel.
// TODO(lambdai): Propose to istio api.
func GetTunnelBuilderType(clusterName string, proxy *model.Proxy, push *model.PushContext) networking.TunnelType {
	if proxy == nil || proxy.Metadata == nil || proxy.Metadata.ProxyConfig == nil {
		return networking.NoTunnel
	}
	if outTunnel, ok := proxy.Metadata.ProxyConfig.ProxyMetadata["tunnel"]; ok {
		switch outTunnel {
		case networking.H2TunnelTypeName:
			return networking.H2Tunnel
		default:
			// passthrough
		}
	}
	return networking.NoTunnel
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
	tunnelType      networking.TunnelType

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
	params := []string{b.clusterName, b.network, b.clusterID, util.LocalityToString(b.locality), b.tunnelType.ToString()}
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

// TODO(lambdai): Receive port value(15009 by default), builder to cover wide cases.
type EndpointTunnelApplier interface {
	// Mutate LbEndpoint in place. Return non-nil on failure.
	ApplyTunnel(lep *endpoint.LbEndpoint, tunnelType networking.TunnelType) (*endpoint.LbEndpoint, error)
}

type EndpointNoTunnelApplier struct{}

// Note that this will not return error if another tunnel typs requested.
func (t *EndpointNoTunnelApplier) ApplyTunnel(lep *endpoint.LbEndpoint, tunnelType networking.TunnelType) (*endpoint.LbEndpoint, error) {
	return lep, nil
}

type EndpointH2TunnelApplier struct{}

// TODO(lambdai): Set original port if the default cluster original port is not the same.
func (t *EndpointH2TunnelApplier) ApplyTunnel(lep *endpoint.LbEndpoint, tunnelType networking.TunnelType) (*endpoint.LbEndpoint, error) {
	switch tunnelType {
	case networking.H2Tunnel:
		if ep := lep.GetEndpoint(); ep != nil {
			if ep.Address.GetSocketAddress().GetPortValue() != 0 {
				newEp := proto.Clone(lep).(*endpoint.LbEndpoint)
				newEp.GetEndpoint().Address.GetSocketAddress().PortSpecifier = &core.SocketAddress_PortValue{
					PortValue: 15009,
				}
				return newEp, nil
			}
		}
		return lep, nil
	case networking.NoTunnel:
		return lep, nil
	default:
		panic("supported tunnel type")
	}
}

type LocLbEndpointsAndOptions struct {
	// The protobuf message which contains LbEndpoint slice.
	llbEndpoints endpoint.LocalityLbEndpoints
	// The runtime information of the LbEndpoint slice. Each LbEndpoint has individual metadata at the same index.
	tunnelMetadata []EndpointTunnelApplier
}

// Return prefer H2 tunnel metadata.
func MakeTunnelApplier(le *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) EndpointTunnelApplier {
	if tunnelOpt.SupportH2Tunnel() {
		return &EndpointH2TunnelApplier{}
	}
	return &EndpointNoTunnelApplier{}
}

func (e *LocLbEndpointsAndOptions) append(le *endpoint.LbEndpoint, tunnelOpt networking.TunnelAbility) {
	e.llbEndpoints.LbEndpoints = append(e.llbEndpoints.LbEndpoints, le)
	e.tunnelMetadata = append(e.tunnelMetadata, MakeTunnelApplier(le, tunnelOpt))
}

func (e *LocLbEndpointsAndOptions) emplace(le *endpoint.LbEndpoint, tunnelMetadata EndpointTunnelApplier) {
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
}

func (e *LocLbEndpointsAndOptions) AssertInvarianceInTest() {
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
	// Extract shard keys so we can iterate in order. This ensures a stable EDS output. Since
	// len(shards) ~= number of remote clusters which isn't too large, doing this sort shouldn't be
	// too problematic. If it becomes an issue we can cache it in the EndpointShards struct.
	keys := make([]string, 0, len(shards.Shards))
	for k := range shards.Shards {
		keys = append(keys, k)
	}
	if len(keys) >= 2 {
		sort.Strings(keys)
	}
	// The shards are updated independently, now need to filter and merge
	// for this cluster
	for _, clusterID := range keys {
		endpoints := shards.Shards[clusterID]
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
					make([]EndpointTunnelApplier, 0, len(endpoints)),
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
	locs := make([]string, 0, len(localityEpMap))
	for k := range localityEpMap {
		locs = append(locs, k)
	}
	if len(locs) >= 2 {
		sort.Strings(locs)
	}
	for _, k := range locs {
		locLbEps := localityEpMap[k]
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

// TODO(lambdai): Handle ApplyTunnel error return value by filter out the failed endpoint.
func (b *EndpointBuilder) ApplyTunnelSetting(llbOpts []*LocLbEndpointsAndOptions, tunnelType networking.TunnelType) []*LocLbEndpointsAndOptions {
	for _, llb := range llbOpts {
		for i, ep := range llb.llbEndpoints.LbEndpoints {
			newEp, err := llb.tunnelMetadata[i].ApplyTunnel(ep, tunnelType)
			if err != nil {
				panic("not implemented yet on failing to apply tunnel")
			} else {
				llb.llbEndpoints.LbEndpoints[i] = newEp
			}
		}
	}
	return llbOpts
}

// Create the CLusterLoadAssignment. At this moment the options must have been applied to the locality lb endpoints.
func (b *EndpointBuilder) createClusterLoadAssignment(llbOpts []*LocLbEndpointsAndOptions) *endpoint.ClusterLoadAssignment {
	llbEndpoints := make([]*endpoint.LocalityLbEndpoints, 0, len(llbOpts))
	for _, l := range llbOpts {
		llbEndpoints = append(llbEndpoints, &l.llbEndpoints)
	}
	return &endpoint.ClusterLoadAssignment{
		ClusterName: b.clusterName,
		Endpoints:   llbEndpoints,
	}
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
	ep.Metadata = util.BuildLbEndpointMetadata(e.Network, e.TLSMode, e.WorkloadName, e.Namespace, e.Locality.ClusterID, e.Labels)

	return ep
}
