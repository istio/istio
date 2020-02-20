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
	"net"
	"sort"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/duration"
	pstruct "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/strcase"
)

const (
	// BlackHoleCluster to catch traffic from routes with unresolved clusters. Traffic arriving here goes nowhere.
	BlackHoleCluster = "BlackHoleCluster"
	// BlackHoleRouteName is the name of the route that blocks all traffic.
	BlackHoleRouteName = "block_all"
	// PassthroughCluster to forward traffic to the original destination requested. This cluster is used when
	// traffic does not match any listener in envoy.
	PassthroughCluster = "PassthroughCluster"
	// PassthroughRouteName is the name of the route that forwards traffic to the
	// PassthroughCluster
	PassthroughRouteName = "allow_any"

	// Inbound pass through cluster need to the bind the loopback ip address for the security and loop avoidance.
	InboundPassthroughClusterIpv4 = "InboundPassthroughClusterIpv4"
	InboundPassthroughClusterIpv6 = "InboundPassthroughClusterIpv6"
	// 6 is the magical number for inbound: 15006, 127.0.0.6, ::6
	InboundPassthroughBindIpv4 = "127.0.0.6"
	InboundPassthroughBindIpv6 = "::6"

	// SniClusterFilter is the name of the sni_cluster envoy filter
	SniClusterFilter = "envoy.filters.network.sni_cluster"
	// ForwardDownstreamSniFilter forwards the sni from downstream connections to upstream
	// Used only in the fallthrough filter stack for TLS connections
	ForwardDownstreamSniFilter = "forward_downstream_sni"
	// IstioMetadataKey is the key under which metadata is added to a route or cluster
	// regarding the virtual service or destination rule used for each
	IstioMetadataKey = "istio"

	// EnvoyTransportSocketMetadataKey is the key under which metadata is added to an endpoint
	// which determines the endpoint level transport socket configuration.
	EnvoyTransportSocketMetadataKey = "envoy.transport_socket_match"

	// EnvoyRawBufferSocketName matched with hardcoded built-in Envoy transport name which determines
	// endpoint level plantext transport socket configuration
	EnvoyRawBufferSocketName = "envoy.transport_sockets.raw_buffer"

	// EnvoyTLSSocketName matched with hardcoded built-in Envoy transport name which determines endpoint
	// level tls transport socket configuration
	EnvoyTLSSocketName = "envoy.transport_sockets.tls"
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

// ALPNInMeshWithMxc advertises that Proxy is going to talk to the in-mesh cluster and has metadata exchange enabled for
// TCP. The custom "istio-peer-exchange" value indicates, metadata exchange is enabled for TCP. The custom "istio" value
// indicates in-mesh traffic and it's going to be used for routing decisions.
var ALPNInMeshWithMxc = []string{"istio-peer-exchange", "istio"}

// ALPNHttp advertises that Proxy is going to talking either http2 or http 1.1.
var ALPNHttp = []string{"h2", "http/1.1"}

// ALPNDownstream advertises that Proxy is going to talking either tcp(for metadata exchange), http2 or http 1.1.
var ALPNDownstream = []string{"istio-peer-exchange", "h2", "http/1.1"}

// FallThroughFilterChainBlackHoleService is the blackhole service used for fall though
// filter chain
var FallThroughFilterChainBlackHoleService = &model.Service{
	Hostname: host.Name(BlackHoleCluster),
	Attributes: model.ServiceAttributes{
		Name: BlackHoleCluster,
	},
}

// FallThroughFilterChainPassthroughService is the passthrough service used for fall though
var FallThroughFilterChainPassthroughService = &model.Service{
	Hostname: host.Name(PassthroughCluster),
	Attributes: model.ServiceAttributes{
		Name: PassthroughCluster,
	},
}

func getMaxCidrPrefix(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip.To4() == nil {
		// ipv6 address
		return 128
	}
	// ipv4 address
	return 32
}

// ConvertAddressToCidr converts from string to CIDR proto
func ConvertAddressToCidr(addr string) *core.CidrRange {
	if len(addr) == 0 {
		return nil
	}

	cidr := &core.CidrRange{
		AddressPrefix: addr,
		PrefixLen: &wrappers.UInt32Value{
			Value: getMaxCidrPrefix(addr),
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
func BuildAddress(bind string, port uint32) *core.Address {
	if len(bind) > 0 && strings.HasPrefix(bind, model.UnixAddressPrefix) {
		return &core.Address{
			Address: &core.Address_Pipe{
				Pipe: &core.Pipe{
					Path: bind,
				},
			},
		}
	}

	return &core.Address{
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

// GetEndpointAddress returns an Envoy v2 API `Address` that represents this IstioEndpoint
func GetEndpointAddress(n *model.IstioEndpoint) *core.Address {
	switch n.Family {
	case model.AddressFamilyTCP:
		return BuildAddress(n.Address, n.EndpointPort)
	case model.AddressFamilyUnix:
		return &core.Address{Address: &core.Address_Pipe{Pipe: &core.Pipe{Path: n.Address}}}
	default:
		panic(fmt.Sprintf("unhandled Family %v", n.Family))
	}
}

// GetByAddress returns a listener by its address
// TODO(mostrowski): consider passing map around to save iteration.
func GetByAddress(listeners []*xdsapi.Listener, addr core.Address) *xdsapi.Listener {
	for _, l := range listeners {
		if l != nil && proto.Equal(l.Address, &addr) {
			return l
		}
	}
	return nil
}

// MessageToAnyWithError converts from proto message to proto Any
func MessageToAnyWithError(msg proto.Message) (*any.Any, error) {
	b := proto.NewBuffer(nil)
	b.SetDeterministic(true)
	err := b.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return &any.Any{
		TypeUrl: "type.googleapis.com/" + proto.MessageName(msg),
		Value:   b.Bytes(),
	}, nil
}

// MessageToAny converts from proto message to proto Any
func MessageToAny(msg proto.Message) *any.Any {
	out, err := MessageToAnyWithError(msg)
	if err != nil {
		log.Error(fmt.Sprintf("error marshaling Any %s: %v", msg.String(), err))
		return nil
	}
	return out
}

// MessageToStruct converts from proto message to proto Struct
func MessageToStruct(msg proto.Message) *pstruct.Struct {
	s, err := conversion.MessageToStruct(msg)
	if err != nil {
		log.Error(err.Error())
		return &pstruct.Struct{}
	}
	return s
}

// GogoDurationToDuration converts from gogo proto duration to time.duration
func GogoDurationToDuration(d *types.Duration) *duration.Duration {
	if d == nil {
		return nil
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		// TODO(mostrowski): add error handling instead.
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
		return nil
	}
	return ptypes.DurationProto(dur)
}

// SortVirtualHosts sorts a slice of virtual hosts by name.
//
// Envoy computes a hash of RDS to see if things have changed - hash is affected by order of elements in the filter. Therefore
// we sort virtual hosts by name before handing them back so the ordering is stable across HTTP Route Configs.
func SortVirtualHosts(hosts []*route.VirtualHost) {
	sort.SliceStable(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
}

// IsIstioVersionGE13 checks whether the given Istio version is greater than or equals 1.3.
func IsIstioVersionGE13(node *model.Proxy) bool {
	return node.IstioVersion == nil ||
		node.IstioVersion.Compare(&model.IstioVersion{Major: 1, Minor: 3, Patch: -1}) >= 0
}

// IsIstioVersionGE14 checks whether the given Istio version is greater than or equals 1.4.
func IsIstioVersionGE14(node *model.Proxy) bool {
	return node.IstioVersion == nil ||
		node.IstioVersion.Compare(&model.IstioVersion{Major: 1, Minor: 4, Patch: -1}) >= 0
}

// IsIstioVersionGE15 checks whether the given Istio version is greater than or equals 1.5.
func IsIstioVersionGE15(node *model.Proxy) bool {
	return node.IstioVersion == nil ||
		node.IstioVersion.Compare(&model.IstioVersion{Major: 1, Minor: 5, Patch: -1}) >= 0
}

// IsProtocolSniffingEnabled checks whether protocol sniffing is enabled.
func IsProtocolSniffingEnabledForOutbound(node *model.Proxy) bool {
	return features.EnableProtocolSniffingForOutbound.Get() && IsIstioVersionGE13(node)
}

func IsProtocolSniffingEnabledForInbound(node *model.Proxy) bool {
	return features.EnableProtocolSniffingForInbound.Get() && IsIstioVersionGE14(node)
}

func IsProtocolSniffingEnabledForPort(node *model.Proxy, port *model.Port) bool {
	return IsProtocolSniffingEnabledForOutbound(node) && port.Protocol.IsUnsupported()
}

func IsProtocolSniffingEnabledForInboundPort(node *model.Proxy, port *model.Port) bool {
	return IsProtocolSniffingEnabledForInbound(node) && port.Protocol.IsUnsupported()
}

func IsProtocolSniffingEnabledForOutboundPort(node *model.Proxy, port *model.Port) bool {
	return IsProtocolSniffingEnabledForOutbound(node) && port.Protocol.IsUnsupported()
}

// IsTCPMetadataExchangeEnabled checks whether Metadata Exchanged enabled for TCP using ALPN.
func IsTCPMetadataExchangeEnabled(node *model.Proxy) bool {
	return features.EnableTCPMetadataExchange.Get() && IsIstioVersionGE15(node)
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func ConvertLocality(locality string) *core.Locality {
	if locality == "" {
		return &core.Locality{}
	}

	region, zone, subzone := SplitLocality(locality)
	return &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func LocalityToString(l *core.Locality) string {
	if l == nil {
		return ""
	}
	resp := l.Region
	if l.Zone == "" {
		return resp
	}
	resp += "/" + l.Zone
	if l.SubZone == "" {
		return resp
	}
	resp += "/" + l.SubZone
	return resp
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
func cloneLocalityLbEndpoints(endpoints []*endpoint.LocalityLbEndpoints) []*endpoint.LocalityLbEndpoints {
	out := make([]*endpoint.LocalityLbEndpoints, 0, len(endpoints))
	for _, ep := range endpoints {
		clone := *ep
		if ep.LoadBalancingWeight != nil {
			clone.LoadBalancingWeight = &wrappers.UInt32Value{
				Value: ep.GetLoadBalancingWeight().GetValue(),
			}
		}
		out = append(out, &clone)
	}
	return out
}

// BuildConfigInfoMetadata builds core.Metadata struct containing the
// name.namespace of the config, the type, etc. Used by Mixer client
// to generate attributes for policy and telemetry.
func BuildConfigInfoMetadata(config model.ConfigMeta) *core.Metadata {
	s := "/apis/" + config.Group + "/" + config.Version + "/namespaces/" + config.Namespace + "/" +
		strcase.CamelCaseToKebabCase(config.Type) + "/" + config.Name
	return &core.Metadata{
		FilterMetadata: map[string]*pstruct.Struct{
			IstioMetadataKey: {
				Fields: map[string]*pstruct.Value{
					"config": {
						Kind: &pstruct.Value_StringValue{
							StringValue: s,
						},
					},
				},
			},
		},
	}
}

// IsHTTPFilterChain returns true if the filter chain contains a HTTP connection manager filter
func IsHTTPFilterChain(filterChain *listener.FilterChain) bool {
	for _, f := range filterChain.Filters {
		if f.Name == xdsutil.HTTPConnectionManager {
			return true
		}
	}
	return false
}

// MergeAnyWithStruct merges a given struct into the given Any typed message by dynamically inferring the
// type of Any, converting the struct into the inferred type, merging the two messages, and then
// marshaling the merged message back into Any.
func MergeAnyWithStruct(a *any.Any, pbStruct *pstruct.Struct) (*any.Any, error) {
	// Assuming that Pilot is compiled with this type [which should always be the case]
	var err error
	var x ptypes.DynamicAny

	// First get an object of type used by this message
	if err = ptypes.UnmarshalAny(a, &x); err != nil {
		return nil, err
	}

	// Create a typed copy. We will convert the user's struct to this type
	temp := proto.Clone(x.Message)
	temp.Reset()
	if err = conversion.StructToMessage(pbStruct, temp); err != nil {
		return nil, err
	}

	// Merge the two typed protos
	proto.Merge(x.Message, temp)
	var retVal *any.Any
	// Convert the merged proto back to any
	if retVal, err = ptypes.MarshalAny(x.Message); err != nil {
		return nil, err
	}

	return retVal, nil
}

// MergeAnyWithAny merges a given any typed message into the given Any typed message by dynamically inferring the
// type of Any
func MergeAnyWithAny(dst *any.Any, src *any.Any) (*any.Any, error) {
	// Assuming that Pilot is compiled with this type [which should always be the case]
	var err error
	var dstX, srcX ptypes.DynamicAny

	// get an object of type used by this message
	if err = ptypes.UnmarshalAny(dst, &dstX); err != nil {
		return nil, err
	}

	// get an object of type used by this message
	if err = ptypes.UnmarshalAny(src, &srcX); err != nil {
		return nil, err
	}

	// Merge the two typed protos
	proto.Merge(dstX.Message, srcX.Message)
	var retVal *any.Any
	// Convert the merged proto back to dst
	if retVal, err = ptypes.MarshalAny(dstX.Message); err != nil {
		return nil, err
	}

	return retVal, nil
}

// BuildLbEndpointMetadata adds metadata values to a lb endpoint
func BuildLbEndpointMetadata(uid string, network string, tlsMode string, push *model.PushContext) *core.Metadata {
	if !push.IsMixerEnabled() {
		// Only use UIDs when Mixer is enabled.
		uid = ""
	}

	if uid == "" && network == "" && tlsMode == model.DisabledTLSModeLabel {
		return nil
	}

	metadata := &core.Metadata{
		FilterMetadata: map[string]*pstruct.Struct{},
	}

	if uid != "" || network != "" {
		metadata.FilterMetadata[IstioMetadataKey] = &pstruct.Struct{
			Fields: map[string]*pstruct.Value{},
		}

		if uid != "" {
			metadata.FilterMetadata[IstioMetadataKey].Fields["uid"] = &pstruct.Value{Kind: &pstruct.Value_StringValue{StringValue: uid}}
		}

		if network != "" {
			metadata.FilterMetadata[IstioMetadataKey].Fields["network"] = &pstruct.Value{Kind: &pstruct.Value_StringValue{StringValue: network}}
		}
	}

	if tlsMode != "" {
		metadata.FilterMetadata[EnvoyTransportSocketMetadataKey] = &pstruct.Struct{
			Fields: map[string]*pstruct.Value{
				model.TLSModeLabelShortname: {Kind: &pstruct.Value_StringValue{StringValue: tlsMode}},
			},
		}
	}

	return metadata
}

// IsAllowAnyOutbound checks if allow_any is enabled for outbound traffic
func IsAllowAnyOutbound(node *model.Proxy) bool {
	return node.SidecarScope != nil &&
		node.SidecarScope.OutboundTrafficPolicy != nil &&
		node.SidecarScope.OutboundTrafficPolicy.Mode == networking.OutboundTrafficPolicy_ALLOW_ANY
}
