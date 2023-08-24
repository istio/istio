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

package util

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	cookiev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/http/stateful_session/cookie/v3"
	headerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/http/stateful_session/header/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/type/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto/merge"
	"istio.io/istio/pkg/util/strcase"
)

const (
	// BlackHoleCluster to catch traffic from routes with unresolved clusters. Traffic arriving here goes nowhere.
	BlackHoleCluster = "BlackHoleCluster"
	// BlackHole is the name of the virtual host and route name used to block all traffic
	BlackHole = "block_all"
	// PassthroughCluster to forward traffic to the original destination requested. This cluster is used when
	// traffic does not match any listener in envoy.
	PassthroughCluster = "PassthroughCluster"
	// Passthrough is the name of the virtual host used to forward traffic to the
	// PassthroughCluster
	Passthrough = "allow_any"

	// PassthroughFilterChain to catch traffic that doesn't match other filter chains.
	PassthroughFilterChain = "PassthroughFilterChain"

	// Inbound pass through cluster need to the bind the loopback ip address for the security and loop avoidance.
	InboundPassthroughClusterIpv4 = "InboundPassthroughClusterIpv4"
	InboundPassthroughClusterIpv6 = "InboundPassthroughClusterIpv6"

	// IstioMetadataKey is the key under which metadata is added to a route or cluster
	// regarding the virtual service or destination rule used for each
	IstioMetadataKey = "istio"

	// EnvoyTransportSocketMetadataKey is the key under which metadata is added to an endpoint
	// which determines the endpoint level transport socket configuration.
	EnvoyTransportSocketMetadataKey = "envoy.transport_socket_match"

	// Well-known header names
	AltSvcHeader = "alt-svc"

	// Envoy Stateful Session Filter
	// TODO: Move to well known.
	StatefulSessionFilter = "envoy.filters.http.stateful_session"

	// AlpnOverrideMetadataKey is the key under which metadata is added
	// to indicate whether Istio rewrite the ALPN headers
	AlpnOverrideMetadataKey = "alpn_override"
)

// ALPNH2Only advertises that Proxy is going to use HTTP/2 when talking to the cluster.
var ALPNH2Only = []string{"h2"}

// ALPNInMeshH2 advertises that Proxy is going to use HTTP/2 when talking to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
// Once Envoy supports client-side ALPN negotiation, this should be {"istio", "h2", "http/1.1"}.
var ALPNInMeshH2 = []string{"istio", "h2"}

// ALPNInMeshH2WithMxc advertises that Proxy is going to use HTTP/2 when talking to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
// The custom "istio-peer-exchange" value indicates, metadata exchange is enabled for TCP.
var ALPNInMeshH2WithMxc = []string{"istio-peer-exchange", "istio", "h2"}

// ALPNInMesh advertises that Proxy is going to talk to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
var ALPNInMesh = []string{"istio"}

// ALPNInMeshWithMxc advertises that Proxy is going to talk to the in-mesh cluster and has metadata exchange enabled for
// TCP. The custom "istio-peer-exchange" value indicates, metadata exchange is enabled for TCP. The custom "istio" value
// indicates in-mesh traffic and it's going to be used for routing decisions.
var ALPNInMeshWithMxc = []string{"istio-peer-exchange", "istio"}

// ALPNHttp advertises that Proxy is going to talking either http2 or http 1.1.
var ALPNHttp = []string{"h2", "http/1.1"}

// ALPNHttp3OverQUIC advertises that Proxy is going to talk HTTP/3 over QUIC
var ALPNHttp3OverQUIC = []string{"h3"}

// ALPNDownstreamWithMxc advertises that Proxy is going to talk either tcp(for metadata exchange), http2 or http 1.1.
var ALPNDownstreamWithMxc = []string{"istio-peer-exchange", "h2", "http/1.1"}

// ConvertAddressToCidr converts from string to CIDR proto
func ConvertAddressToCidr(addr string) *core.CidrRange {
	cidr, err := AddrStrToCidrRange(addr)
	if err != nil {
		log.Errorf("failed to convert address %s to CidrRange: %v", addr, err)
		return nil
	}

	return cidr
}

// AddrStrToCidrRange converts from string to CIDR proto
func AddrStrToCidrRange(addr string) (*core.CidrRange, error) {
	if len(addr) == 0 {
		return nil, fmt.Errorf("empty address")
	}

	var (
		ipAddr        netip.Addr
		maxCidrPrefix int
	)

	if strings.Contains(addr, "/") {
		ipp, err := netip.ParsePrefix(addr)
		if err != nil {
			return nil, err
		}
		ipAddr = ipp.Addr()
		maxCidrPrefix = ipp.Bits()
	} else {
		ipa, err := netip.ParseAddr(addr)
		if err != nil {
			return nil, err
		}

		ipAddr = ipa
		maxCidrPrefix = ipAddr.BitLen()
	}

	return &core.CidrRange{
		AddressPrefix: ipAddr.String(),
		PrefixLen: &wrapperspb.UInt32Value{
			Value: uint32(maxCidrPrefix),
		},
	}, nil
}

// BuildAddress returns a SocketAddress with the given ip and port or uds.
func BuildAddress(bind string, port uint32) *core.Address {
	address := BuildNetworkAddress(bind, port, istionetworking.TransportProtocolTCP)
	if address != nil {
		return address
	}

	return &core.Address{
		Address: &core.Address_Pipe{
			Pipe: &core.Pipe{
				Path: strings.TrimPrefix(bind, model.UnixAddressPrefix),
			},
		},
	}
}

// BuildAdditionalAddresses can add extra addresses to additional addresses for a listener
func BuildAdditionalAddresses(extrAddresses []string, listenPort uint32, node *model.Proxy) []*listener.AdditionalAddress {
	var additionalAddresses []*listener.AdditionalAddress
	if len(extrAddresses) > 0 {
		for _, exbd := range extrAddresses {
			if exbd == "" {
				continue
			}
			extraAddress := &listener.AdditionalAddress{
				Address: BuildAddress(exbd, listenPort),
			}
			additionalAddresses = append(additionalAddresses, extraAddress)
		}
	}
	return additionalAddresses
}

func BuildNetworkAddress(bind string, port uint32, transport istionetworking.TransportProtocol) *core.Address {
	if port == 0 {
		return nil
	}
	return &core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address:  bind,
				Protocol: transport.ToEnvoySocketProtocol(),
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: port,
				},
			},
		},
	}
}

// SortVirtualHosts sorts a slice of virtual hosts by name.
//
// Envoy computes a hash of RDS to see if things have changed - hash is affected by order of elements in the filter. Therefore
// we sort virtual hosts by name before handing them back so the ordering is stable across HTTP Route Configs.
func SortVirtualHosts(hosts []*route.VirtualHost) {
	if len(hosts) < 2 {
		return
	}
	sort.SliceStable(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})
}

// IsIstioVersionGE117 checks whether the given Istio version is greater than or equals 1.17.
func IsIstioVersionGE117(version *model.IstioVersion) bool {
	return version == nil ||
		version.Compare(&model.IstioVersion{Major: 1, Minor: 17, Patch: -1}) >= 0
}

// IsIstioVersionGE119 checks whether the given Istio version is greater than or equals 1.19.
func IsIstioVersionGE119(version *model.IstioVersion) bool {
	return version == nil ||
		version.Compare(&model.IstioVersion{Major: 1, Minor: 19, Patch: -1}) >= 0
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func ConvertLocality(locality string) *core.Locality {
	if locality == "" {
		return &core.Locality{}
	}

	region, zone, subzone := label.SplitLocalityLabel(locality)
	return &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}
}

// LocalityToString converts Locality struct to '/' separated locality string.
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

// GetFailoverPriorityLabels returns a byte array which contains failover priorities of the proxy.
func GetFailoverPriorityLabels(proxyLabels map[string]string, priorities []string) []byte {
	var b bytes.Buffer
	for _, key := range priorities {
		b.WriteString(key)
		b.WriteRune(':')
		b.WriteString(proxyLabels[key])
		b.WriteRune(' ')
	}
	return b.Bytes()
}

// IsLocalityEmpty checks if a locality is empty (checking region is good enough, based on how its initialized)
func IsLocalityEmpty(locality *core.Locality) bool {
	if locality == nil || (len(locality.GetRegion()) == 0) {
		return true
	}
	return false
}

func LocalityMatch(proxyLocality *core.Locality, ruleLocality string) bool {
	ruleRegion, ruleZone, ruleSubzone := label.SplitLocalityLabel(ruleLocality)
	regionMatch := ruleRegion == "*" || proxyLocality.GetRegion() == ruleRegion
	zoneMatch := ruleZone == "*" || ruleZone == "" || proxyLocality.GetZone() == ruleZone
	subzoneMatch := ruleSubzone == "*" || ruleSubzone == "" || proxyLocality.GetSubZone() == ruleSubzone

	if regionMatch && zoneMatch && subzoneMatch {
		return true
	}
	return false
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

// return a shallow copy ClusterLoadAssignment
func CloneClusterLoadAssignment(original *endpoint.ClusterLoadAssignment) *endpoint.ClusterLoadAssignment {
	if original == nil {
		return nil
	}
	out := &endpoint.ClusterLoadAssignment{}

	out.ClusterName = original.ClusterName
	out.Endpoints = cloneLocalityLbEndpoints(original.Endpoints)
	out.Policy = original.Policy

	return out
}

// return a shallow copy LocalityLbEndpoints
func cloneLocalityLbEndpoints(endpoints []*endpoint.LocalityLbEndpoints) []*endpoint.LocalityLbEndpoints {
	out := make([]*endpoint.LocalityLbEndpoints, 0, len(endpoints))
	for _, ep := range endpoints {
		clone := CloneLocalityLbEndpoint(ep)
		out = append(out, clone)
	}
	return out
}

// return a shallow copy of LocalityLbEndpoints
func CloneLocalityLbEndpoint(ep *endpoint.LocalityLbEndpoints) *endpoint.LocalityLbEndpoints {
	clone := &endpoint.LocalityLbEndpoints{}
	clone.Locality = ep.Locality
	clone.LbEndpoints = ep.LbEndpoints
	clone.Proximity = ep.Proximity
	clone.Priority = ep.Priority
	if ep.LoadBalancingWeight != nil {
		clone.LoadBalancingWeight = &wrapperspb.UInt32Value{
			Value: ep.GetLoadBalancingWeight().GetValue(),
		}
	}
	return clone
}

// BuildConfigInfoMetadata builds core.Metadata struct containing the
// name.namespace of the config, the type, etc.
func BuildConfigInfoMetadata(config config.Meta) *core.Metadata {
	return AddConfigInfoMetadata(nil, config)
}

// AddConfigInfoMetadata adds name.namespace of the config, the type, etc
// to the given core.Metadata struct, if metadata is not initialized, build a new metadata.
func AddConfigInfoMetadata(metadata *core.Metadata, config config.Meta) *core.Metadata {
	if metadata == nil {
		metadata = &core.Metadata{
			FilterMetadata: map[string]*structpb.Struct{},
		}
	}
	s := "/apis/" + config.GroupVersionKind.Group + "/" + config.GroupVersionKind.Version + "/namespaces/" + config.Namespace + "/" +
		strcase.CamelCaseToKebabCase(config.GroupVersionKind.Kind) + "/" + config.Name
	if _, ok := metadata.FilterMetadata[IstioMetadataKey]; !ok {
		metadata.FilterMetadata[IstioMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}
	metadata.FilterMetadata[IstioMetadataKey].Fields["config"] = &structpb.Value{
		Kind: &structpb.Value_StringValue{
			StringValue: s,
		},
	}
	return metadata
}

// AddSubsetToMetadata will insert the subset name supplied. This should be called after the initial
// "istio" metadata has been created for the cluster. If the "istio" metadata field is not already
// defined, the subset information will not be added (to prevent adding this information where not
// needed). This is used for telemetry reporting.
func AddSubsetToMetadata(md *core.Metadata, subset string) {
	if istioMeta, ok := md.FilterMetadata[IstioMetadataKey]; ok {
		istioMeta.Fields["subset"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{
				StringValue: subset,
			},
		}
	}
}

// AddALPNOverrideToMetadata sets filter metadata `istio.alpn_override: "false"` in the given core.Metadata struct,
// when TLS mode is SIMPLE or MUTUAL. If metadata is not initialized, builds a new metadata.
func AddALPNOverrideToMetadata(metadata *core.Metadata, tlsMode networking.ClientTLSSettings_TLSmode) *core.Metadata {
	if tlsMode != networking.ClientTLSSettings_SIMPLE && tlsMode != networking.ClientTLSSettings_MUTUAL {
		return metadata
	}

	if metadata == nil {
		metadata = &core.Metadata{
			FilterMetadata: map[string]*structpb.Struct{},
		}
	}

	if _, ok := metadata.FilterMetadata[IstioMetadataKey]; !ok {
		metadata.FilterMetadata[IstioMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}

	metadata.FilterMetadata[IstioMetadataKey].Fields["alpn_override"] = &structpb.Value{
		Kind: &structpb.Value_StringValue{
			StringValue: "false",
		},
	}

	return metadata
}

// IsHTTPFilterChain returns true if the filter chain contains a HTTP connection manager filter
func IsHTTPFilterChain(filterChain *listener.FilterChain) bool {
	for _, f := range filterChain.Filters {
		if f.Name == wellknown.HTTPConnectionManager {
			return true
		}
	}
	return false
}

// MergeAnyWithAny merges a given any typed message into the given Any typed message by dynamically inferring the
// type of Any
func MergeAnyWithAny(dst *anypb.Any, src *anypb.Any) (*anypb.Any, error) {
	// Assuming that Pilot is compiled with this type [which should always be the case]
	var err error

	// get an object of type used by this message
	dstX, err := dst.UnmarshalNew()
	if err != nil {
		return nil, err
	}

	// get an object of type used by this message
	srcX, err := src.UnmarshalNew()
	if err != nil {
		return nil, err
	}

	// Merge the two typed protos
	merge.Merge(dstX, srcX)

	// Convert the merged proto back to dst
	retVal := protoconv.MessageToAny(dstX)

	return retVal, nil
}

// AppendLbEndpointMetadata adds metadata values to a lb endpoint using the passed in metadata as base.
func AppendLbEndpointMetadata(istioMetadata *model.EndpointMetadata, envoyMetadata *core.Metadata,
) {
	if !features.EndpointTelemetryLabel || !features.EnableTelemetryLabel {
		return
	}

	if envoyMetadata.FilterMetadata == nil {
		envoyMetadata.FilterMetadata = map[string]*structpb.Struct{}
	}

	if istioMetadata.TLSMode != "" && istioMetadata.TLSMode != model.DisabledTLSModeLabel {
		envoyMetadata.FilterMetadata[EnvoyTransportSocketMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: istioMetadata.TLSMode}},
			},
		}
	}

	// Add compressed telemetry metadata. Note this is a short term solution to make server workload metadata
	// available at client sidecar, so that telemetry filter could use for metric labels. This is useful for two cases:
	// server does not have sidecar injected, and request fails to reach server and thus metadata exchange does not happen.
	// Due to performance concern, telemetry metadata is compressed into a semicolon separated string:
	// workload-name;namespace;canonical-service-name;canonical-service-revision;cluster-id.
	if features.EndpointTelemetryLabel {
		// allow defaulting for non-injected cases
		canonicalName, canonicalRevision := kubelabels.CanonicalService(istioMetadata.Labels, istioMetadata.WorkloadName)

		// don't bother sending the default value in config
		if canonicalRevision == "latest" {
			canonicalRevision = ""
		}

		var sb strings.Builder
		sb.WriteString(istioMetadata.WorkloadName)
		sb.WriteString(";")
		sb.WriteString(istioMetadata.Namespace)
		sb.WriteString(";")
		sb.WriteString(canonicalName)
		sb.WriteString(";")
		sb.WriteString(canonicalRevision)
		sb.WriteString(";")
		sb.WriteString(istioMetadata.ClusterID.String())
		addIstioEndpointLabel(envoyMetadata, "workload", &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: sb.String()}})
	}
}

func addIstioEndpointLabel(metadata *core.Metadata, key string, val *structpb.Value) {
	if _, ok := metadata.FilterMetadata[IstioMetadataKey]; !ok {
		metadata.FilterMetadata[IstioMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}

	metadata.FilterMetadata[IstioMetadataKey].Fields[key] = val
}

// IsAllowAnyOutbound checks if allow_any is enabled for outbound traffic
func IsAllowAnyOutbound(node *model.Proxy) bool {
	return node.SidecarScope != nil &&
		node.SidecarScope.OutboundTrafficPolicy != nil &&
		node.SidecarScope.OutboundTrafficPolicy.Mode == networking.OutboundTrafficPolicy_ALLOW_ANY
}

func StringToExactMatch(in []string) []*matcher.StringMatcher {
	if len(in) == 0 {
		return nil
	}
	res := make([]*matcher.StringMatcher, 0, len(in))
	for _, s := range in {
		res = append(res, &matcher.StringMatcher{
			MatchPattern: &matcher.StringMatcher_Exact{Exact: s},
		})
	}
	return res
}

func StringToPrefixMatch(in []string) []*matcher.StringMatcher {
	if len(in) == 0 {
		return nil
	}
	res := make([]*matcher.StringMatcher, 0, len(in))
	for _, s := range in {
		res = append(res, &matcher.StringMatcher{
			MatchPattern: &matcher.StringMatcher_Prefix{Prefix: s},
		})
	}
	return res
}

func ConvertToEnvoyMatches(in []*networking.StringMatch) []*matcher.StringMatcher {
	res := make([]*matcher.StringMatcher, 0, len(in))

	for _, im := range in {
		if em := ConvertToEnvoyMatch(im); em != nil {
			res = append(res, em)
		}
	}

	return res
}

func ConvertToEnvoyMatch(in *networking.StringMatch) *matcher.StringMatcher {
	switch m := in.MatchType.(type) {
	case *networking.StringMatch_Exact:
		return &matcher.StringMatcher{MatchPattern: &matcher.StringMatcher_Exact{Exact: m.Exact}}
	case *networking.StringMatch_Prefix:
		return &matcher.StringMatcher{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: m.Prefix}}
	case *networking.StringMatch_Regex:
		return &matcher.StringMatcher{
			MatchPattern: &matcher.StringMatcher_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					Regex: m.Regex,
				},
			},
		}
	}
	return nil
}

func CidrRangeSliceEqual(a, b []*core.CidrRange) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		netA, err := toMaskedPrefix(a[i])
		if err != nil {
			return false
		}
		netB, err := toMaskedPrefix(b[i])
		if err != nil {
			return false
		}
		if netA.Addr().String() != netB.Addr().String() {
			return false
		}
	}

	return true
}

func toMaskedPrefix(c *core.CidrRange) (netip.Prefix, error) {
	ipp, err := netip.ParsePrefix(c.AddressPrefix + "/" + strconv.Itoa(int(c.PrefixLen.GetValue())))
	if err != nil {
		log.Errorf("failed to parse CidrRange %v as IPNet: %v", c, err)
	}

	return ipp.Masked(), err
}

// meshconfig ForwardClientCertDetails and the Envoy config enum are off by 1
// due to the UNDEFINED in the meshconfig ForwardClientCertDetails
func MeshConfigToEnvoyForwardClientCertDetails(c meshconfig.ForwardClientCertDetails) hcm.HttpConnectionManager_ForwardClientCertDetails {
	return hcm.HttpConnectionManager_ForwardClientCertDetails(c - 1)
}

// ByteCount returns a human readable byte format
// Inspired by https://yourbasic.org/golang/formatting-byte-size-to-human-readable-format/
func ByteCount(b int) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

// IPv6Compliant encloses ipv6 addresses in square brackets followed by port number in Host header/URIs
func IPv6Compliant(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// DomainName builds the domain name for a given host and port
func DomainName(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// BuildInternalEndpoint builds an lb endpoint pointing to the internal listener named dest.
// If the metadata contains "tunnel.destination" that will become the "endpointId" to prevent deduplication.
func BuildInternalEndpoint(dest string, meta *core.Metadata) []*endpoint.LocalityLbEndpoints {
	llb := []*endpoint.LocalityLbEndpoints{{
		LbEndpoints: []*endpoint.LbEndpoint{BuildInternalLbEndpoint(dest, meta)},
	}}
	return llb
}

// BuildInternalLbEndpoint builds an lb endpoint pointing to the internal listener named dest.
// If the metadata contains "tunnel.destination" that will become the "endpointId" to prevent deduplication.
func BuildInternalLbEndpoint(dest string, meta *core.Metadata) *endpoint.LbEndpoint {
	var endpointID string
	if tunnel, ok := meta.GetFilterMetadata()["tunnel"]; ok {
		if dest, ok := tunnel.GetFields()["destination"]; ok {
			endpointID = dest.GetStringValue()
		}
	}
	address := BuildInternalAddressWithIdentifier(dest, endpointID)

	return &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: address,
			},
		},
		Metadata: meta,
	}
}

func BuildInternalAddressWithIdentifier(name, identifier string) *core.Address {
	return &core.Address{
		Address: &core.Address_EnvoyInternalAddress{
			EnvoyInternalAddress: &core.EnvoyInternalAddress{
				AddressNameSpecifier: &core.EnvoyInternalAddress_ServerListenerName{
					ServerListenerName: name,
				},
				EndpointId: identifier,
			},
		},
	}
}

func BuildTunnelMetadata(address string, port, tunnelPort int) *core.Metadata {
	return &core.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"tunnel": BuildTunnelMetadataStruct(address, address, port, tunnelPort),
		},
	}
}

func BuildTunnelMetadataStruct(tunnelAddress, address string, port, tunnelPort int) *structpb.Struct {
	st, _ := structpb.NewStruct(map[string]interface{}{
		// ORIGINAL_DST destination address to tunnel on (usually only differs from "destination" by port)
		"address": net.JoinHostPort(tunnelAddress, strconv.Itoa(tunnelPort)),
		// logical destination behind the tunnel, on which policy and telemetry will be applied
		"destination": net.JoinHostPort(address, strconv.Itoa(port)),
	})
	return st
}

func BuildStatefulSessionFilter(svc *model.Service) *hcm.HttpFilter {
	filterConfig := MaybeBuildStatefulSessionFilterConfig(svc)
	if filterConfig == nil {
		return nil
	}

	return &hcm.HttpFilter{
		Name: StatefulSessionFilter,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(filterConfig),
		},
	}
}

func MaybeBuildStatefulSessionFilterConfig(svc *model.Service) *statefulsession.StatefulSession {
	if svc == nil {
		return nil
	}
	sessionCookie := svc.Attributes.Labels[features.PersistentSessionLabel]
	sessionHeader := svc.Attributes.Labels[features.PersistentSessionHeaderLabel]

	switch {
	case sessionCookie != "":
		cookieName, cookiePath, found := strings.Cut(sessionCookie, ":")
		if !found {
			cookiePath = "/"
		}
		return &statefulsession.StatefulSession{
			SessionState: &core.TypedExtensionConfig{
				Name: "envoy.http.stateful_session.cookie",
				TypedConfig: protoconv.MessageToAny(&cookiev3.CookieBasedSessionState{
					Cookie: &httpv3.Cookie{
						Path: cookiePath,
						Ttl:  &durationpb.Duration{Seconds: 120},
						Name: cookieName,
					},
				}),
			},
		}
	case sessionHeader != "":
		return &statefulsession.StatefulSession{
			SessionState: &core.TypedExtensionConfig{
				Name: "envoy.http.stateful_session.header",
				TypedConfig: protoconv.MessageToAny(&headerv3.HeaderBasedSessionState{
					Name: sessionHeader,
				}),
			},
		}
	}
	return nil
}
