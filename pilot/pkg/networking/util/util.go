// Copyright 2017 Istio Authors
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

package util

import (
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/log"
)

const (
	// BlackHoleCluster to catch traffic from routes with unresolved clusters. Traffic arriving here goes nowhere.
	BlackHoleCluster = "BlackHoleCluster"
	// PassthroughCluster to forward traffic to the original destination requested. This cluster is used when
	// traffic does not match any listener in envoy.
	PassthroughCluster = "PassthroughCluster"
	// SniClusterFilter is the name of the sni_cluster envoy filter
	SniClusterFilter = "envoy.filters.network.sni_cluster"
	// NoProxyLocality represents the locality associated with a proxy that doesn't have locality settings
	// since all our localities are in region/zone/subzone format, the empty locality will be of form
	// '///'
	NoProxyLocality = "///"
	// IstioMetadataKey is the key under which metadata is added to a route or cluster
	// regarding the virtual service or destination rule used for each
	IstioMetadataKey = "istio"
	// The range of LoadBalancingWeight is [1, 128]
	maxLoadBalancingWeight = 128
)

// ALPNH2Only advertises that Proxy is going to use HTTP/2 when talking to the cluster.
var ALPNH2Only = []string{"h2"}

// ALPNInMeshH2 advertises that Proxy is going to use HTTP/2 when talking to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
// Once Envoy supports client-side ALPN negotiation, this should be {"istio", "h2", "http/1.1"}.
var ALPNInMeshH2 = []string{"istio", "h2"}

// ALPNInMesh advertises that Proxy is going to talk to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
var ALPNInMesh = []string{"istio"}

// ALPNHttp advertises that Proxy is going to talking either http2 or http 1.1.
var ALPNHttp = []string{"h2", "http/1.1"}

// ConvertAddressToCidr converts from string to CIDR proto
func ConvertAddressToCidr(addr string) *core.CidrRange {
	if len(addr) == 0 {
		return nil
	}

	cidr := &core.CidrRange{
		AddressPrefix: addr,
		PrefixLen: &types.UInt32Value{
			Value: 32,
		},
	}

	if strings.Contains(addr, "/") {
		parts := strings.Split(addr, "/")
		cidr.AddressPrefix = parts[0]
		prefix, _ := strconv.Atoi(parts[1])
		cidr.PrefixLen.Value = uint32(prefix)
	}
	return cidr
}

// BuildAddress returns a SocketAddress with the given ip and port or uds.
func BuildAddress(bind string, port uint32) core.Address {
	if len(bind) > 0 && strings.HasPrefix(bind, model.UnixAddressPrefix) {
		return core.Address{
			Address: &core.Address_Pipe{
				Pipe: &core.Pipe{
					Path: bind,
				},
			},
		}
	}

	return core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address: bind,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: port,
				},
			},
		},
	}
}

// GetNetworkEndpointAddress returns an Envoy v2 API `Address` that represents this NetworkEndpoint
func GetNetworkEndpointAddress(n *model.NetworkEndpoint) core.Address {
	switch n.Family {
	case model.AddressFamilyTCP:
		return BuildAddress(n.Address, uint32(n.Port))
	case model.AddressFamilyUnix:
		return core.Address{Address: &core.Address_Pipe{Pipe: &core.Pipe{Path: n.Address}}}
	default:
		panic(fmt.Sprintf("unhandled Family %v", n.Family))
	}
}

// lbWeightNormalize set LbEndpoints within a locality with a valid LoadBalancingWeight.
func lbWeightNormalize(endpoints []endpoint.LbEndpoint) []endpoint.LbEndpoint {
	var totalLbEndpointsNum uint32
	var needNormalize bool

	for _, ep := range endpoints {
		if ep.GetLoadBalancingWeight().GetValue() > maxLoadBalancingWeight {
			needNormalize = true
		}
		totalLbEndpointsNum += ep.GetLoadBalancingWeight().GetValue()
	}
	if !needNormalize {
		return endpoints
	}

	out := make([]endpoint.LbEndpoint, len(endpoints))
	for i, ep := range endpoints {
		weight := float64(ep.GetLoadBalancingWeight().GetValue()*maxLoadBalancingWeight) / float64(totalLbEndpointsNum)
		ep.LoadBalancingWeight = &types.UInt32Value{
			Value: uint32(math.Ceil(weight)),
		}
		out[i] = ep
	}

	return out
}

// LocalityLbWeightNormalize set LocalityLbEndpoints within a cluster with a valid LoadBalancingWeight.
func LocalityLbWeightNormalize(endpoints []endpoint.LocalityLbEndpoints) []endpoint.LocalityLbEndpoints {
	var totalLbEndpointsNum uint32
	var needNormalize bool

	for i, localityLbEndpoint := range endpoints {
		if localityLbEndpoint.GetLoadBalancingWeight().GetValue() > maxLoadBalancingWeight {
			needNormalize = true
		}
		totalLbEndpointsNum += localityLbEndpoint.GetLoadBalancingWeight().GetValue()
		endpoints[i].LbEndpoints = lbWeightNormalize(localityLbEndpoint.LbEndpoints)
	}
	if !needNormalize {
		return endpoints
	}

	out := make([]endpoint.LocalityLbEndpoints, len(endpoints))
	for i, localityLbEndpoint := range endpoints {
		weight := float64(localityLbEndpoint.GetLoadBalancingWeight().GetValue()*maxLoadBalancingWeight) / float64(totalLbEndpointsNum)
		localityLbEndpoint.LoadBalancingWeight = &types.UInt32Value{
			Value: uint32(math.Ceil(weight)),
		}
		out[i] = localityLbEndpoint
	}

	return out
}

// GetByAddress returns a listener by its address
// TODO(mostrowski): consider passing map around to save iteration.
func GetByAddress(listeners []*xdsapi.Listener, addr string) *xdsapi.Listener {
	for _, listener := range listeners {
		if listener != nil && listener.Address.String() == addr {
			return listener
		}
	}
	return nil
}

// MessageToAny converts from proto message to proto Any
func MessageToAny(msg proto.Message) *types.Any {
	s, err := types.MarshalAny(msg)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return s
}

// MessageToStruct converts from proto message to proto Struct
func MessageToStruct(msg proto.Message) *types.Struct {
	s, err := util.MessageToStruct(msg)
	if err != nil {
		log.Error(err.Error())
		return &types.Struct{}
	}
	return s
}

// GogoDurationToDuration converts from gogo proto duration to time.duration
func GogoDurationToDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		// TODO(mostrowski): add error handling instead.
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
		return 0
	}
	return dur
}

// SortVirtualHosts sorts a slice of virtual hosts by name.
//
// Envoy computes a hash of RDS to see if things have changed - hash is affected by order of elements in the filter. Therefore
// we sort virtual hosts by name before handing them back so the ordering is stable across HTTP Route Configs.
func SortVirtualHosts(hosts []route.VirtualHost) {
	sort.SliceStable(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
}

// IsProxyVersionGE11 checks whether the given Proxy version is greater than or equals 1.1.
func IsProxyVersionGE11(node *model.Proxy) bool {
	ver, _ := node.GetProxyVersion()
	return ver >= "1.1"
}

// IsXDSMarshalingToAnyEnabled controls whether "marshaling to Any" feature is enabled.
func IsXDSMarshalingToAnyEnabled(node *model.Proxy) bool {
	return IsProxyVersionGE11(node) && !pilot.DisableXDSMarshalingToAny()
}

// ResolveHostsInNetworksConfig will go through the Gateways addresses for all
// networks in the config and if it's not an IP address it will try to lookup
// that hostname and replace it with the IP address in the config
func ResolveHostsInNetworksConfig(config *meshconfig.MeshNetworks) {
	if config == nil {
		return
	}
	for _, n := range config.Networks {
		for _, gw := range n.Gateways {
			gwIP := net.ParseIP(gw.GetAddress())
			if gwIP == nil {
				addrs, err := net.LookupHost(gw.GetAddress())
				if err == nil && len(addrs) > 0 {
					gw.Gw = &meshconfig.Network_IstioNetworkGateway_Address{
						Address: addrs[0],
					}
				}
			}
		}
	}
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func ConvertLocality(locality string) *core.Locality {
	if locality == "" {
		return nil
	}

	region, zone, subzone := SplitLocality(locality)
	return &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}
}

// IsLocalityEmpty checks if a locality is empty (checking region is good enough, based on how its initialized)
func IsLocalityEmpty(locality *core.Locality) bool {
	if locality == nil || (len(locality.GetRegion()) == 0) {
		return true
	}
	return false
}

func LocalityMatch(proxyLocality *core.Locality, ruleLocality string) bool {
	ruleRegion, ruleZone, ruleSubzone := SplitLocality(ruleLocality)
	regionMatch := ruleRegion == "*" || proxyLocality.GetRegion() == ruleRegion
	zoneMatch := ruleZone == "*" || ruleZone == "" || proxyLocality.GetZone() == ruleZone
	subzoneMatch := ruleSubzone == "*" || ruleSubzone == "" || proxyLocality.GetSubZone() == ruleSubzone

	if regionMatch && zoneMatch && subzoneMatch {
		return true
	}
	return false
}

func SplitLocality(locality string) (region, zone, subzone string) {
	items := strings.Split(locality, "/")
	switch len(items) {
	case 1:
		return items[0], "", ""
	case 2:
		return items[0], items[1], ""
	default:
		return items[0], items[1], items[2]
	}
}

func LbPriority(proxyLocality, endpointsLocality *core.Locality) int {
	if proxyLocality.GetRegion() == endpointsLocality.GetRegion() {
		if proxyLocality.GetZone() == endpointsLocality.GetZone() {
			if proxyLocality.GetSubZone() == endpointsLocality.GetSubZone() {
				return 0
			}
			return 1
		}
		return 2
	}
	return 3
}

// return a shallow copy cluster
func CloneCluster(cluster *xdsapi.Cluster) xdsapi.Cluster {
	out := xdsapi.Cluster{}
	if cluster == nil {
		return out
	}

	out = *cluster
	loadAssignment := CloneClusterLoadAssignment(cluster.LoadAssignment)
	out.LoadAssignment = &loadAssignment

	return out
}

// return a shallow copy ClusterLoadAssignment
func CloneClusterLoadAssignment(original *xdsapi.ClusterLoadAssignment) xdsapi.ClusterLoadAssignment {
	out := xdsapi.ClusterLoadAssignment{}
	if original == nil {
		return out
	}

	out = *original
	out.Endpoints = cloneLocalityLbEndpoints(out.Endpoints)

	return out
}

// return a shallow copy LocalityLbEndpoints
func cloneLocalityLbEndpoints(endpoints []endpoint.LocalityLbEndpoints) []endpoint.LocalityLbEndpoints {
	out := make([]endpoint.LocalityLbEndpoints, 0, len(endpoints))
	for _, ep := range endpoints {
		clone := ep
		if ep.LoadBalancingWeight != nil {
			clone.LoadBalancingWeight = &types.UInt32Value{
				Value: ep.GetLoadBalancingWeight().GetValue(),
			}
		}
		out = append(out, clone)
	}
	return out
}

// BuildConfigInfoMetadata builds core.Metadata struct containing the
// name.namespace of the config, the type, etc. Used by Mixer client
// to generate attributes for policy and telemetry.
func BuildConfigInfoMetadata(config model.ConfigMeta) *core.Metadata {
	return &core.Metadata{
		FilterMetadata: map[string]*types.Struct{
			IstioMetadataKey: {
				Fields: map[string]*types.Value{
					"config": {
						Kind: &types.Value_StringValue{
							StringValue: fmt.Sprintf("/apis/%s/%s/namespaces/%s/%s/%s", config.Group, config.Version, config.Namespace, config.Type, config.Name),
						},
					},
				},
			},
		},
	}
}

// IsHTTPFilterChain returns true if the filter chain contains a HTTP connection manager filter
func IsHTTPFilterChain(filterChain listener.FilterChain) bool {
	for _, f := range filterChain.Filters {
		if f.Name == xdsutil.HTTPConnectionManager {
			return true
		}
	}
	return false
}
