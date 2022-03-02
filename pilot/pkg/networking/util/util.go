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
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/util/strcase"
	"istio.io/pkg/log"
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

	// SniClusterFilter is the name of the sni_cluster envoy filter
	SniClusterFilter = "envoy.filters.network.sni_cluster"

	// IstioMetadataKey is the key under which metadata is added to a route or cluster
	// regarding the virtual service or destination rule used for each
	IstioMetadataKey = "istio"

	// EnvoyTransportSocketMetadataKey is the key under which metadata is added to an endpoint
	// which determines the endpoint level transport socket configuration.
	EnvoyTransportSocketMetadataKey = "envoy.transport_socket_match"

	// EnvoyRawBufferSocketName matched with hardcoded built-in Envoy transport name which determines
	// endpoint level plantext transport socket configuration
	EnvoyRawBufferSocketName = wellknown.TransportSocketRawBuffer

	// EnvoyTLSSocketName matched with hardcoded built-in Envoy transport name which determines endpoint
	// level tls transport socket configuration
	EnvoyTLSSocketName = wellknown.TransportSocketTls

	// EnvoyQUICSocketName matched with hardcoded built-in Envoy transport name which determines endpoint
	// level QUIC transport socket configuration
	EnvoyQUICSocketName = wellknown.TransportSocketQuic

	// StatName patterns
	serviceStatPattern         = "%SERVICE%"
	serviceFQDNStatPattern     = "%SERVICE_FQDN%"
	servicePortStatPattern     = "%SERVICE_PORT%"
	servicePortNameStatPattern = "%SERVICE_PORT_NAME%"
	subsetNameStatPattern      = "%SUBSET_NAME%"

	// Well-known header names
	AltSvcHeader = "alt-svc"
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

// ALPNDownstream advertises that Proxy is going to talk http2 or http 1.1.
var ALPNDownstream = []string{"h2", "http/1.1"}

func getMaxCidrPrefix(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip.To4() == nil {
		// ipv6 address
		return 128
	}
	// ipv4 address
	return 32
}

func ListContains(haystack []string, needle string) bool {
	for _, n := range haystack {
		if needle == n {
			return true
		}
	}
	return false
}

// ConvertAddressToCidr converts from string to CIDR proto
func ConvertAddressToCidr(addr string) *core.CidrRange {
	if len(addr) == 0 {
		return nil
	}

	cidr := &core.CidrRange{
		AddressPrefix: addr,
		PrefixLen: &wrapperspb.UInt32Value{
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

// MessageToAnyWithError converts from proto message to proto Any
func MessageToAnyWithError(msg proto.Message) (*anypb.Any, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return &anypb.Any{
		// nolint: staticcheck
		TypeUrl: "type.googleapis.com/" + string(msg.ProtoReflect().Descriptor().FullName()),
		Value:   b,
	}, nil
}

// MessageToAny converts from proto message to proto Any
func MessageToAny(msg proto.Message) *anypb.Any {
	out, err := MessageToAnyWithError(msg)
	if err != nil {
		log.Error(fmt.Sprintf("error marshaling Any %s: %v", prototext.Format(msg), err))
		return nil
	}
	return out
}

// GogoDurationToDuration converts from gogo proto duration to time.duration
func GogoDurationToDuration(d *types.Duration) *durationpb.Duration {
	if d == nil {
		return nil
	}

	return &durationpb.Duration{
		Seconds: d.Seconds,
		Nanos:   d.Nanos,
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

func IsProtocolSniffingEnabledForPort(port *model.Port) bool {
	return features.EnableProtocolSniffingForOutbound && port.Protocol.IsUnsupported()
}

func IsProtocolSniffingEnabledForInboundPort(port *model.Port) bool {
	return features.EnableProtocolSniffingForInbound && port.Protocol.IsUnsupported()
}

func IsProtocolSniffingEnabledForOutboundPort(port *model.Port) bool {
	return features.EnableProtocolSniffingForOutbound && port.Protocol.IsUnsupported()
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func ConvertLocality(locality string) *core.Locality {
	if locality == "" {
		return &core.Locality{}
	}

	region, zone, subzone := model.SplitLocalityLabel(locality)
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

// IsLocalityEmpty checks if a locality is empty (checking region is good enough, based on how its initialized)
func IsLocalityEmpty(locality *core.Locality) bool {
	if locality == nil || (len(locality.GetRegion()) == 0) {
		return true
	}
	return false
}

func LocalityMatch(proxyLocality *core.Locality, ruleLocality string) bool {
	ruleRegion, ruleZone, ruleSubzone := model.SplitLocalityLabel(ruleLocality)
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
	proto.Merge(dstX, srcX)
	var retVal *anypb.Any

	// Convert the merged proto back to dst
	if retVal, err = anypb.New(dstX); err != nil {
		return nil, err
	}

	return retVal, nil
}

// BuildLbEndpointMetadata adds metadata values to a lb endpoint
func BuildLbEndpointMetadata(networkID network.ID, tlsMode, workloadname, namespace string,
	clusterID cluster.ID, labels labels.Instance) *core.Metadata {
	if networkID == "" && (tlsMode == "" || tlsMode == model.DisabledTLSModeLabel) &&
		(!features.EndpointTelemetryLabel || !features.EnableTelemetryLabel) {
		return nil
	}

	metadata := &core.Metadata{
		FilterMetadata: map[string]*structpb.Struct{},
	}

	if tlsMode != "" && tlsMode != model.DisabledTLSModeLabel {
		metadata.FilterMetadata[EnvoyTransportSocketMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: tlsMode}},
			},
		}
	}

	// Add compressed telemetry metadata. Note this is a short term solution to make server workload metadata
	// available at client sidecar, so that telemetry filter could use for metric labels. This is useful for two cases:
	// server does not have sidecar injected, and request fails to reach server and thus metadata exchange does not happen.
	// Due to performance concern, telemetry metadata is compressed into a semicolon separted string:
	// workload-name;namespace;canonical-service-name;canonical-service-revision;cluster-id.
	if features.EndpointTelemetryLabel {
		var sb strings.Builder
		sb.WriteString(workloadname)
		sb.WriteString(";")
		sb.WriteString(namespace)
		sb.WriteString(";")
		if csn, ok := labels[model.IstioCanonicalServiceLabelName]; ok {
			sb.WriteString(csn)
		}
		sb.WriteString(";")
		if csr, ok := labels[model.IstioCanonicalServiceRevisionLabelName]; ok {
			sb.WriteString(csr)
		}
		sb.WriteString(";")
		sb.WriteString(clusterID.String())
		addIstioEndpointLabel(metadata, "workload", &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: sb.String()}})
	}

	return metadata
}

// MaybeApplyTLSModeLabel may or may not update the metadata for the Envoy transport socket matches for auto mTLS.
func MaybeApplyTLSModeLabel(ep *endpoint.LbEndpoint, tlsMode string) (*endpoint.LbEndpoint, bool) {
	if ep == nil || ep.Metadata == nil {
		return nil, false
	}
	epTLSMode := ""
	if ep.Metadata.FilterMetadata != nil {
		if v, ok := ep.Metadata.FilterMetadata[EnvoyTransportSocketMetadataKey]; ok {
			epTLSMode = v.Fields[model.TLSModeLabelShortname].GetStringValue()
		}
	}
	// Normalize the tls label name before comparison. This ensure we won't falsely cloning
	// the endpoint when they are "" and model.DisabledTLSModeLabel.
	if epTLSMode == model.DisabledTLSModeLabel {
		epTLSMode = ""
	}
	if tlsMode == model.DisabledTLSModeLabel {
		tlsMode = ""
	}
	if epTLSMode == tlsMode {
		return nil, false
	}
	// We make a copy instead of modifying on existing endpoint pointer directly to avoid data race.
	// See https://github.com/istio/istio/issues/34227 for details.
	newEndpoint := proto.Clone(ep).(*endpoint.LbEndpoint)
	if tlsMode != "" && tlsMode != model.DisabledTLSModeLabel {
		newEndpoint.Metadata.FilterMetadata[EnvoyTransportSocketMetadataKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: tlsMode}},
			},
		}
	} else {
		delete(newEndpoint.Metadata.FilterMetadata, EnvoyTransportSocketMetadataKey)
	}
	return newEndpoint, true
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

// BuildStatPrefix builds a stat prefix based on the stat pattern.
func BuildStatPrefix(statPattern string, host string, subset string, port *model.Port, attributes *model.ServiceAttributes) string {
	prefix := strings.ReplaceAll(statPattern, serviceStatPattern, shortHostName(host, attributes))
	prefix = strings.ReplaceAll(prefix, serviceFQDNStatPattern, host)
	prefix = strings.ReplaceAll(prefix, subsetNameStatPattern, subset)
	prefix = strings.ReplaceAll(prefix, servicePortStatPattern, strconv.Itoa(port.Port))
	prefix = strings.ReplaceAll(prefix, servicePortNameStatPattern, port.Name)
	return prefix
}

// shortHostName constructs the name from kubernetes hosts based on attributes (name and namespace).
// For other hosts like VMs, this method does not do any thing - just returns the passed in host as is.
func shortHostName(host string, attributes *model.ServiceAttributes) string {
	if attributes.ServiceRegistry == provider.Kubernetes {
		return attributes.Name + "." + attributes.Namespace
	}
	return host
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

func StringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func UInt32SliceEqual(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func CidrRangeSliceEqual(a, b []*core.CidrRange) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		netA, err := toIPNet(a[i])
		if err != nil {
			return false
		}
		netB, err := toIPNet(b[i])
		if err != nil {
			return false
		}
		if netA.IP.String() != netB.IP.String() {
			return false
		}
	}

	return true
}

func toIPNet(c *core.CidrRange) (*net.IPNet, error) {
	_, cA, err := net.ParseCIDR(c.AddressPrefix + "/" + strconv.Itoa(int(c.PrefixLen.GetValue())))
	if err != nil {
		log.Errorf("failed to parse CidrRange %v as IPNet: %v", c, err)
	}
	return cA, err
}

// meshconfig ForwardClientCertDetails and the Envoy config enum are off by 1
// due to the UNDEFINED in the meshconfig ForwardClientCertDetails
func MeshConfigToEnvoyForwardClientCertDetails(c meshconfig.Topology_ForwardClientCertDetails) http_conn.HttpConnectionManager_ForwardClientCertDetails {
	return http_conn.HttpConnectionManager_ForwardClientCertDetails(c - 1)
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

// IPv6 addresses are enclosed in square brackets followed by port number in Host header/URIs
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

// TraceOperation builds the string format: "%s:%d/*" for a given host and port
func TraceOperation(host string, port int) string {
	// Format : "%s:%d/*"
	return DomainName(host, port) + "/*"
}
