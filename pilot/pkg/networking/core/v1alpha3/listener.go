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

package v1alpha3

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	accesslogconfig "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	grpc_stats "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/grpc_stats/v2alpha"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	thrift_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/thrift_proxy/v2alpha1"
	thrift_ratelimit "github.com/envoyproxy/go-control-plane/envoy/config/filter/thrift/rate_limit/v2alpha1"
	ratelimit "github.com/envoyproxy/go-control-plane/envoy/config/ratelimit/v2"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/util/gogo"
	alpn_filter "istio.io/istio/security/proto/envoy/config/filter/http/alpn/v2alpha1"
	"istio.io/pkg/monitoring"
)

const (
	NoConflict = iota
	// Incoming HTTP existing HTTP
	HTTPOverHTTP
	// Incoming HTTP existing TCP
	HTTPOverTCP
	// Incoming HTTP existing AUTO
	HTTPOverAuto
	// Incoming TCP existing HTTP
	TCPOverHTTP
	// Incoming TCP existing TCP
	TCPOverTCP
	// Incoming TCP existing AUTO
	TCPOverAuto
	// Incoming AUTO existing HTTP
	AutoOverHTTP
	// Incoming AUTO existing TCP
	AutoOverTCP
	// Incoming AUTO existing AUTO
	AutoOverAuto
)

const (
	// RDSHttpProxy is the special name for HTTP PROXY route
	RDSHttpProxy = "http_proxy"

	// VirtualOutboundListenerName is the name for traffic capture listener
	VirtualOutboundListenerName = "virtualOutbound"

	// VirtualInboundListenerName is the name for traffic capture listener
	VirtualInboundListenerName = "virtualInbound"

	// WildcardAddress binds to all IP addresses
	WildcardAddress = "0.0.0.0"

	// WildcardIPv6Address binds to all IPv6 addresses
	WildcardIPv6Address = "::"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"

	// LocalhostIPv6Address for local binding
	LocalhostIPv6Address = "::1"

	// EnvoyTextLogFormat12 format for envoy text based access logs for Istio 1.2
	EnvoyTextLogFormat12 = "[%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% " +
		"%PROTOCOL%\" %RESPONSE_CODE% %RESPONSE_FLAGS% \"%DYNAMIC_METADATA(istio.mixer:status)%\" " +
		"\"%UPSTREAM_TRANSPORT_FAILURE_REASON%\" %BYTES_RECEIVED% %BYTES_SENT% " +
		"%DURATION% %RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)% \"%REQ(X-FORWARDED-FOR)%\" " +
		"\"%REQ(USER-AGENT)%\" \"%REQ(X-REQUEST-ID)%\" \"%REQ(:AUTHORITY)%\" \"%UPSTREAM_HOST%\" " +
		"%UPSTREAM_CLUSTER% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
		"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME%\n"

	// EnvoyTextLogFormat13 format for envoy text based access logs for Istio 1.3 onwards
	EnvoyTextLogFormat13 = "[%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% " +
		"%PROTOCOL%\" %RESPONSE_CODE% %RESPONSE_FLAGS% \"%DYNAMIC_METADATA(istio.mixer:status)%\" " +
		"\"%UPSTREAM_TRANSPORT_FAILURE_REASON%\" %BYTES_RECEIVED% %BYTES_SENT% " +
		"%DURATION% %RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)% \"%REQ(X-FORWARDED-FOR)%\" " +
		"\"%REQ(USER-AGENT)%\" \"%REQ(X-REQUEST-ID)%\" \"%REQ(:AUTHORITY)%\" \"%UPSTREAM_HOST%\" " +
		"%UPSTREAM_CLUSTER% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
		"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME% %ROUTE_NAME%\n"

	// EnvoyServerName for istio's envoy
	EnvoyServerName = "istio-envoy"

	httpEnvoyAccessLogFriendlyName = "http_envoy_accesslog"
	tcpEnvoyAccessLogFriendlyName  = "tcp_envoy_accesslog"

	tcpEnvoyALSName = "envoy.tcp_grpc_access_log"

	// EnvoyAccessLogCluster is the cluster name that has details for server implementing Envoy ALS.
	// This cluster is created in bootstrap.
	EnvoyAccessLogCluster = "envoy_accesslog_service"

	// ProxyInboundListenPort is the port on which all inbound traffic to the pod/vm will be captured to
	// TODO: allow configuration through mesh config
	ProxyInboundListenPort = 15006

	// Used in xds config. Metavalue bind to this key is used by pilot as xds server but not by envoy.
	// So the meta data can be erased when pushing to envoy.
	PilotMetaKey = "pilot_meta"

	// CanonicalHTTPSPort defines the standard port for HTTPS traffic. To avoid conflicts, http services
	// are not allowed on this port.
	CanonicalHTTPSPort = 443

	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"

	ThriftRLSDefaultTimeoutMS = 50
)

type FilterChainMatchOptions struct {
	// Application protocols of the filter chain match
	ApplicationProtocols []string
	// Transport protocol of the filter chain match. "tls" or empty
	TransportProtocol string
	// Filter chain protocol. HTTP for HTTP proxy and TCP for TCP proxy
	Protocol plugin.ListenerProtocol
}

// A set of pre-allocated variables related to protocol sniffing logic for
// propagating the ALPN to upstreams
var (
	// These are sniffed by the HTTP Inspector in the outbound listener
	// We need to forward these ALPNs to upstream so that the upstream can
	// properly use a HTTP or TCP listener
	plaintextHTTPALPNs = []string{"http/1.0", "http/1.1", "h2c"}
	mtlsHTTPALPNs      = []string{"istio-http/1.0", "istio-http/1.1", "istio-h2"}

	mtlsTCPALPNs        = []string{"istio"}
	mtlsTCPWithMxcALPNs = []string{"istio-peer-exchange", "istio"}

	// These ALPNs are injected in the client side by the ALPN filter.
	// "istio" is added for each upstream protocol in order to make it
	// backward compatible. e.g., 1.4 proxy -> 1.3 proxy.
	mtlsHTTP10ALPN = []string{"istio-http/1.0", "istio"}
	mtlsHTTP11ALPN = []string{"istio-http/1.1", "istio"}
	mtlsHTTP2ALPN  = []string{"istio-h2", "istio"}

	// Double the number of filter chains. Half of filter chains are used as http filter chain and half of them are used as tcp proxy
	// id in [0, len(allChains)/2) are configured as http filter chain, [(len(allChains)/2, len(allChains)) are configured as tcp proxy
	// If mTLS permissive is enabled, there are five filter chains. The filter chain match should be
	//  FCM 1: ALPN [istio-http/1.0, istio-http/1.1, istio-h2] Transport protocol: tls      --> HTTP traffic from sidecar over TLS
	//  FCM 2: ALPN [http/1.0, http/1.1, h2c] Transport protocol: N/A                       --> HTTP traffic over plain text
	//  FCM 3: ALPN [istio] Transport protocol: tls                                         --> TCP traffic from sidecar over TLS
	//  FCM 4: ALPN [] Transport protocol: N/A                                              --> TCP traffic over plain text
	//  FCM 5: ALPN [] Transport protocol: tls                                              --> TCP traffic over TLS
	// If traffic is over plain text or mTLS is strict mode, there are two filter chains. The filter chain match should be
	//  FCM 1: ALPN [http/1.0, http/1.1, h2c, istio-http/1.0, istio-http/1.1, istio-h2]     --> HTTP traffic over plain text or TLS
	//  FCM 2: ALPN []                                                                      --> TCP traffic over plain text or TLS
	inboundPermissiveFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener, sent over mTLS
			ApplicationProtocols: mtlsHTTPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolHTTP,
		},
		{
			// client side traffic was detected as HTTP by the outbound listener, sent out as plain text
			ApplicationProtocols: plaintextHTTPALPNs,
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol: plugin.ListenerProtocolHTTP,
		},
		{
			// client side traffic could not be identified by the outbound listener, but sent over mTLS
			ApplicationProtocols: mtlsTCPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolTCP,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over plaintext
			// or it could be that the client has no sidecar. In this case, this filter chain is simply
			// receiving plaintext TCP traffic.
			Protocol: plugin.ListenerProtocolTCP,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over one-way
			// TLS (HTTPS for example) by the downstream application.
			// or it could be that the client has no sidecar, and it is directly making a HTTPS connection to
			// this sidecar. In this case, this filter chain is receiving plaintext one-way TLS traffic. The TLS
			// inspector would detect this as TLS traffic [not necessarily mTLS]. But since there is no ALPN to match,
			// this filter chain match will treat the traffic as just another TCP proxy.
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolTCP,
		},
	}

	// Same as inboundPermissiveFilterChainMatchOptions except for following case:
	// FCM 3: ALPN [istio-peer-exchange, istio] Transport protocol: tls            --> TCP traffic from sidecar over TLS
	inboundPermissiveFilterChainMatchWithMxcOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener, sent over mTLS
			ApplicationProtocols: mtlsHTTPALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolHTTP,
		},
		{
			// client side traffic was detected as HTTP by the outbound listener, sent out as plain text
			ApplicationProtocols: plaintextHTTPALPNs,
			// No transport protocol match as this filter chain (+match) will be used for plain text connections
			Protocol: plugin.ListenerProtocolHTTP,
		},
		{
			// client side traffic could not be identified by the outbound listener, but sent over mTLS
			ApplicationProtocols: mtlsTCPWithMxcALPNs,
			// If client sends mTLS traffic, transport protocol will be set by the TLS inspector
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolTCP,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over plaintext
			// or it could be that the client has no sidecar. In this case, this filter chain is simply
			// receiving plaintext TCP traffic.
			Protocol: plugin.ListenerProtocolTCP,
		},
		{
			// client side traffic could not be identified by the outbound listener, sent over one-way
			// TLS (HTTPS for example) by the downstream application.
			// or it could be that the client has no sidecar, and it is directly making a HTTPS connection to
			// this sidecar. In this case, this filter chain is receiving plaintext one-way TLS traffic. The TLS
			// inspector would detect this as TLS traffic [not necessarily mTLS]. But since there is no ALPN to match,
			// this filter chain match will treat the traffic as just another TCP proxy.
			TransportProtocol: "tls",
			Protocol:          plugin.ListenerProtocolTCP,
		},
	}

	inboundStrictFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			// client side traffic was detected as HTTP by the outbound listener.
			// If we are in strict mode, we will get mTLS HTTP ALPNS only.
			ApplicationProtocols: mtlsHTTPALPNs,
			Protocol:             plugin.ListenerProtocolHTTP,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol: plugin.ListenerProtocolTCP,
		},
	}

	inboundPlainTextFilterChainMatchOptions = []FilterChainMatchOptions{
		{
			ApplicationProtocols: plaintextHTTPALPNs,
			Protocol:             plugin.ListenerProtocolHTTP,
		},
		{
			// Could not detect traffic on the client side. Server side has no mTLS.
			Protocol: plugin.ListenerProtocolTCP,
		},
	}

	// State logged by the metadata exchange filter about the upstream and downstream service instances
	// We need to propagate these as part of access log service stream
	// Logging them by default on the console may be an issue as the base64 encoded string is bound to be a big one.
	// But end users can certainly configure it on their own via the meshConfig using the %FILTERSTATE% macro.
	envoyWasmStateToLog = []string{"envoy.wasm.metadata_exchange.upstream", "envoy.wasm.metadata_exchange.upstream_id",
		"envoy.wasm.metadata_exchange.downstream", "envoy.wasm.metadata_exchange.downstream_id"}

	// EnvoyJSONLogFormat12 map of values for envoy json based access logs for Istio 1.2
	EnvoyJSONLogFormat12 = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"start_time":                        {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
			"method":                            {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
			"path":                              {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
			"protocol":                          {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
			"response_code":                     {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
			"response_flags":                    {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
			"bytes_received":                    {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
			"bytes_sent":                        {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
			"duration":                          {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
			"upstream_service_time":             {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
			"x_forwarded_for":                   {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
			"user_agent":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
			"request_id":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
			"authority":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
			"upstream_host":                     {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
			"upstream_cluster":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
			"upstream_local_address":            {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
			"downstream_local_address":          {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
			"downstream_remote_address":         {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
			"requested_server_name":             {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
			"istio_policy_status":               {Kind: &structpb.Value_StringValue{StringValue: "%DYNAMIC_METADATA(istio.mixer:status)%"}},
			"upstream_transport_failure_reason": {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_TRANSPORT_FAILURE_REASON%"}},
		},
	}

	// EnvoyJSONLogFormat13 map of values for envoy json based access logs for Istio 1.3 onwards
	EnvoyJSONLogFormat13 = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"start_time":                        {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
			"route_name":                        {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
			"method":                            {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
			"path":                              {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
			"protocol":                          {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
			"response_code":                     {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
			"response_flags":                    {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
			"bytes_received":                    {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
			"bytes_sent":                        {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
			"duration":                          {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
			"upstream_service_time":             {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
			"x_forwarded_for":                   {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
			"user_agent":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
			"request_id":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
			"authority":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
			"upstream_host":                     {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
			"upstream_cluster":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
			"upstream_local_address":            {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
			"downstream_local_address":          {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
			"downstream_remote_address":         {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
			"requested_server_name":             {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
			"istio_policy_status":               {Kind: &structpb.Value_StringValue{StringValue: "%DYNAMIC_METADATA(istio.mixer:status)%"}},
			"upstream_transport_failure_reason": {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_TRANSPORT_FAILURE_REASON%"}},
		},
	}
)

func buildAccessLog(node *model.Proxy, fl *accesslogconfig.FileAccessLog, push *model.PushContext) {
	switch push.Mesh.AccessLogEncoding {
	case meshconfig.MeshConfig_TEXT:
		formatString := EnvoyTextLogFormat12
		if util.IsIstioVersionGE13(node) {
			formatString = EnvoyTextLogFormat13
		}

		if push.Mesh.AccessLogFormat != "" {
			formatString = push.Mesh.AccessLogFormat
		}
		fl.AccessLogFormat = &accesslogconfig.FileAccessLog_Format{
			Format: formatString,
		}
	case meshconfig.MeshConfig_JSON:
		var jsonLog *structpb.Struct
		// TODO potential optimization to avoid recomputing the user provided format for every listener
		// mesh AccessLogFormat field could change so need a way to have a cached value that can be cleared
		// on changes
		if push.Mesh.AccessLogFormat != "" {
			jsonFields := map[string]string{}
			err := json.Unmarshal([]byte(push.Mesh.AccessLogFormat), &jsonFields)
			if err == nil {
				jsonLog = &structpb.Struct{
					Fields: make(map[string]*structpb.Value, len(jsonFields)),
				}
				for key, value := range jsonFields {
					jsonLog.Fields[key] = &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: value}}
				}
			} else {
				log.Errorf("error parsing provided json log format, default log format will be used: %v", err)
			}
		}
		if jsonLog == nil {
			if util.IsIstioVersionGE13(node) {
				jsonLog = EnvoyJSONLogFormat13
			} else {
				jsonLog = EnvoyJSONLogFormat12
			}
		}
		fl.AccessLogFormat = &accesslogconfig.FileAccessLog_JsonFormat{
			JsonFormat: jsonLog,
		}
	default:
		log.Warnf("unsupported access log format %v", push.Mesh.AccessLogEncoding)
	}
}

var (
	// TODO: gauge should be reset on refresh, not the best way to represent errors but better
	// than nothing.
	// TODO: add dimensions - namespace of rule, service, rule name
	invalidOutboundListeners = monitoring.NewGauge(
		"pilot_invalid_out_listeners",
		"Number of invalid outbound listeners.",
	)
)

func init() {
	monitoring.MustRegister(invalidOutboundListeners)
}

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func (configgen *ConfigGeneratorImpl) BuildListeners(node *model.Proxy,
	push *model.PushContext) []*xdsapi.Listener {
	builder := NewListenerBuilder(node)

	switch node.Type {
	case model.SidecarProxy:
		builder = configgen.buildSidecarListeners(node, push, builder)
	case model.Router:
		builder = configgen.buildGatewayListeners(node, push, builder)
	}

	builder.patchListeners(push)
	return builder.getListeners()
}

// buildSidecarListeners produces a list of listeners for sidecar proxies
func (configgen *ConfigGeneratorImpl) buildSidecarListeners(
	node *model.Proxy,
	push *model.PushContext,
	builder *ListenerBuilder) *ListenerBuilder {

	if push.Mesh.ProxyListenPort > 0 {
		// Any build order change need a careful code review
		builder.buildSidecarInboundListeners(configgen, node, push).
			buildSidecarOutboundListeners(configgen, node, push).
			buildManagementListeners(configgen, node, push).
			buildVirtualOutboundListener(configgen, node, push).
			buildVirtualInboundListener(configgen, node, push)
	}

	return builder
}

// buildSidecarInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service proxyInstances.
func (configgen *ConfigGeneratorImpl) buildSidecarInboundListeners(
	node *model.Proxy,
	push *model.PushContext) []*xdsapi.Listener {

	var listeners []*xdsapi.Listener
	listenerMap := make(map[string]*inboundListenerEntry)

	sidecarScope := node.SidecarScope
	noneMode := node.GetInterceptionMode() == model.InterceptionNone

	if !sidecarScope.HasCustomIngressListeners {
		// There is no user supplied sidecarScope for this namespace
		// Construct inbound listeners in the usual way by looking at the ports of the service instances
		// attached to the proxy
		// We should not create inbound listeners in NONE mode based on the service instances
		// Doing so will prevent the workloads from starting as they would be listening on the same port
		// Users are required to provide the sidecar config to define the inbound listeners
		if noneMode {
			return nil
		}

		// inbound connections/requests are redirected to the endpoint address but appear to be sent
		// to the service address.
		//
		// Protocol sniffing for inbound listener.
		// If there is no ingress listener, for each service instance, the listener port protocol is determined
		// by the service port protocol. If user doesn't specify the service port protocol, the listener will
		// be generated using protocol sniffing.
		// For example, the set of service instances
		//      --> Endpoint
		//              Address:Port 172.16.0.1:1111
		//              ServicePort  80|HTTP
		//      --> Endpoint
		//              Address:Port 172.16.0.1:2222
		//              ServicePort  8888|TCP
		//      --> Endpoint
		//              Address:Port 172.16.0.1:3333
		//              ServicePort 9999|Unknown
		//
		//	The pilot will generate three listeners, the last one will use protocol sniffing.
		//
		for _, instance := range node.ServiceInstances {
			endpoint := instance.Endpoint
			bind := endpoint.Address

			// Local service instances can be accessed through one of three
			// addresses: localhost, endpoint IP, and service
			// VIP. Localhost bypasses the proxy and doesn't need any TCP
			// route config. Endpoint IP is handled below and Service IP is handled
			// by outbound routes.
			// Traffic sent to our service VIP is redirected by remote
			// services' kubeproxy to our specific endpoint IP.
			listenerOpts := buildListenerOpts{
				push:       push,
				proxy:      node,
				bind:       bind,
				port:       int(endpoint.EndpointPort),
				bindToPort: false,
			}

			pluginParams := &plugin.InputParams{
				ListenerProtocol: plugin.ModelProtocolToListenerProtocol(node, instance.ServicePort.Protocol,
					core.TrafficDirection_INBOUND),
				DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_INBOUND,
				Node:                       node,
				ServiceInstance:            instance,
				Port:                       instance.ServicePort,
				Push:                       push,
				Bind:                       bind,
			}

			if l := configgen.buildSidecarInboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap); l != nil {
				listeners = append(listeners, l)
			}
		}

	} else {
		rule := sidecarScope.Config.Spec.(*networking.Sidecar)
		for _, ingressListener := range rule.Ingress {
			// determine the bindToPort setting for listeners. Validation guarantees that these are all IP listeners.
			bindToPort := false
			if noneMode {
				// do not care what the listener's capture mode setting is. The proxy does not use iptables
				bindToPort = true
			} else if ingressListener.CaptureMode == networking.CaptureMode_NONE {
				// proxy uses iptables redirect or tproxy. IF mode is not set
				// for older proxies, it defaults to iptables redirect.  If the
				// listener's capture mode specifies NONE, then the proxy wants
				// this listener alone to be on a physical port. If the
				// listener's capture mode is default, then its same as
				// iptables i.e. bindToPort is false.
				bindToPort = true
			}

			listenPort := &model.Port{
				Port:     int(ingressListener.Port.Number),
				Protocol: protocol.Parse(ingressListener.Port.Protocol),
				Name:     ingressListener.Port.Name,
			}

			bind := ingressListener.Bind
			if len(bind) == 0 {
				// User did not provide one. Pick the proxy's IP or wildcard inbound listener.
				bind = getSidecarInboundBindIP(node)
			}

			instance := configgen.findOrCreateServiceInstance(node.ServiceInstances, ingressListener,
				sidecarScope.Config.Name, sidecarScope.Config.Namespace)

			listenerOpts := buildListenerOpts{
				push:       push,
				proxy:      node,
				bind:       bind,
				port:       listenPort.Port,
				bindToPort: bindToPort,
			}

			// we don't need to set other fields of the endpoint here as
			// the consumers of this service instance (listener/filter chain constructors)
			// are simply looking for the service port and the service associated with the instance.
			instance.ServicePort = listenPort

			// Validation ensures that the protocol specified in Sidecar.ingress
			// is always a valid known protocol
			pluginParams := &plugin.InputParams{
				ListenerProtocol: plugin.ModelProtocolToListenerProtocol(node, listenPort.Protocol,
					core.TrafficDirection_INBOUND),
				DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_INBOUND,
				Node:                       node,
				ServiceInstance:            instance,
				Port:                       listenPort,
				Push:                       push,
				Bind:                       bind,
			}

			if l := configgen.buildSidecarInboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap); l != nil {
				listeners = append(listeners, l)
			}
		}
	}

	return listeners
}

func (configgen *ConfigGeneratorImpl) buildSidecarInboundHTTPListenerOptsForPortOrUDS(node *model.Proxy, pluginParams *plugin.InputParams) *httpListenerOpts {
	clusterName := pluginParams.InboundClusterName
	if clusterName == "" {
		// In case of unix domain sockets, the service port will be 0. So use the port name to distinguish the
		// inbound listeners that a user specifies in Sidecar. Otherwise, all inbound clusters will be the same.
		// We use the port name as the subset in the inbound cluster for differentiation. Its fine to use port
		// names here because the inbound clusters are not referred to anywhere in the API, unlike the outbound
		// clusters and these are static endpoint clusters used only for sidecar (proxy -> app)
		clusterName = model.BuildSubsetKey(model.TrafficDirectionInbound, pluginParams.ServiceInstance.ServicePort.Name,
			pluginParams.ServiceInstance.Service.Hostname, pluginParams.ServiceInstance.ServicePort.Port)
	}

	httpOpts := &httpListenerOpts{
		routeConfig: configgen.buildSidecarInboundHTTPRouteConfig(pluginParams.Node,
			pluginParams.Push, pluginParams.ServiceInstance, clusterName),
		rds:              "", // no RDS for inbound traffic
		useRemoteAddress: false,
		connectionManager: &http_conn.HttpConnectionManager{
			// Append and forward client cert to backend.
			ForwardClientCertDetails: http_conn.HttpConnectionManager_APPEND_FORWARD,
			SetCurrentClientCertDetails: &http_conn.HttpConnectionManager_SetCurrentClientCertDetails{
				Subject: proto.BoolTrue,
				Uri:     true,
				Dns:     true,
			},
			ServerName: EnvoyServerName,
		},
	}
	// See https://github.com/grpc/grpc-web/tree/master/net/grpc/gateway/examples/helloworld#configure-the-proxy
	if pluginParams.ServiceInstance.ServicePort.Protocol.IsHTTP2() {
		httpOpts.connectionManager.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
		if pluginParams.ServiceInstance.ServicePort.Protocol == protocol.GRPCWeb {
			httpOpts.addGRPCWebFilter = true
		}
	}

	if features.HTTP10 || node.Metadata.HTTP10 == "1" {
		httpOpts.connectionManager.HttpProtocolOptions = &core.Http1ProtocolOptions{
			AcceptHttp_10: true,
		}
	}

	return httpOpts
}

func (configgen *ConfigGeneratorImpl) buildSidecarThriftListenerOptsForPortOrUDS(pluginParams *plugin.InputParams) *thriftListenerOpts {
	clusterName := pluginParams.InboundClusterName
	if clusterName == "" {
		// In case of unix domain sockets, the service port will be 0. So use the port name to distinguish the
		// inbound listeners that a user specifies in Sidecar. Otherwise, all inbound clusters will be the same.
		// We use the port name as the subset in the inbound cluster for differentiation. Its fine to use port
		// names here because the inbound clusters are not referred to anywhere in the API, unlike the outbound
		// clusters and these are static endpoint clusters used only for sidecar (proxy -> app)
		clusterName = model.BuildSubsetKey(model.TrafficDirectionInbound, pluginParams.ServiceInstance.Endpoint.ServicePortName,
			pluginParams.ServiceInstance.Service.Hostname, int(pluginParams.ServiceInstance.Endpoint.EndpointPort))
	}

	thriftOpts := &thriftListenerOpts{
		transport:   thrift_proxy.TransportType_AUTO_TRANSPORT,
		protocol:    thrift_proxy.ProtocolType_AUTO_PROTOCOL,
		routeConfig: configgen.buildSidecarThriftRouteConfig(clusterName, pluginParams.Push.Mesh.ThriftConfig.RateLimitUrl),
	}

	return thriftOpts
}

// buildSidecarInboundListenerForPortOrUDS creates a single listener on the server-side (inbound)
// for a given port or unix domain socket
func (configgen *ConfigGeneratorImpl) buildSidecarInboundListenerForPortOrUDS(node *model.Proxy, listenerOpts buildListenerOpts,
	pluginParams *plugin.InputParams, listenerMap map[string]*inboundListenerEntry) *xdsapi.Listener {
	// Local service instances can be accessed through one of four addresses:
	// unix domain socket, localhost, endpoint IP, and service
	// VIP. Localhost bypasses the proxy and doesn't need any TCP
	// route config. Endpoint IP is handled below and Service IP is handled
	// by outbound routes. Traffic sent to our service VIP is redirected by
	// remote services' kubeproxy to our specific endpoint IP.
	listenerMapKey := fmt.Sprintf("%s:%d", listenerOpts.bind, listenerOpts.port)

	if old, exists := listenerMap[listenerMapKey]; exists {
		// For sidecar specified listeners, the caller is expected to supply a dummy service instance
		// with the right port and a hostname constructed from the sidecar config's name+namespace
		pluginParams.Push.AddMetric(model.ProxyStatusConflictInboundListener, pluginParams.Node.ID, pluginParams.Node,
			fmt.Sprintf("Conflicting inbound listener:%s. existing: %s, incoming: %s", listenerMapKey,
				old.instanceHostname, pluginParams.ServiceInstance.Service.Hostname))

		// Skip building listener for the same ip port
		return nil
	}

	var allChains []plugin.FilterChain
	for _, p := range configgen.Plugins {
		chains := p.OnInboundFilterChains(pluginParams)
		allChains = append(allChains, chains...)
	}

	if len(allChains) == 0 {
		// add one empty entry to the list so we generate a default listener below
		allChains = []plugin.FilterChain{{}}
	}

	tlsInspectorEnabled := false
	hasTLSContext := false
allChainsLabel:
	for _, c := range allChains {
		for _, lf := range c.ListenerFilters {
			if lf.Name == wellknown.TlsInspector {
				tlsInspectorEnabled = true
				break allChainsLabel
			}
		}

		hasTLSContext = hasTLSContext || c.TLSContext != nil
	}

	var filterChainMatchOption []FilterChainMatchOptions
	// Detect protocol by sniffing and double the filter chain
	if pluginParams.ListenerProtocol == plugin.ListenerProtocolAuto {
		allChains = append(allChains, allChains...)
		if tlsInspectorEnabled {
			allChains = append(allChains, plugin.FilterChain{})
			if util.IsTCPMetadataExchangeEnabled(node) {
				filterChainMatchOption = inboundPermissiveFilterChainMatchWithMxcOptions
			} else {
				filterChainMatchOption = inboundPermissiveFilterChainMatchOptions
			}

		} else {
			if hasTLSContext {
				filterChainMatchOption = inboundStrictFilterChainMatchOptions
			} else {
				filterChainMatchOption = inboundPlainTextFilterChainMatchOptions
			}
		}
		listenerOpts.needHTTPInspector = true
	}

	// name all the filter chains

	for id, chain := range allChains {
		var httpOpts *httpListenerOpts
		var thriftOpts *thriftListenerOpts
		var tcpNetworkFilters []*listener.Filter
		var filterChainMatch *listener.FilterChainMatch

		switch pluginParams.ListenerProtocol {
		case plugin.ListenerProtocolHTTP:
			filterChainMatch = chain.FilterChainMatch
			if filterChainMatch != nil && len(filterChainMatch.ApplicationProtocols) > 0 {
				// This is the filter chain used by permissive mTLS. Append mtlsHTTPALPNs as the client side will
				// override the ALPN with mtlsHTTPALPNs.
				// TODO: This should move to authN code instead of us appending additional ALPNs here.
				filterChainMatch.ApplicationProtocols = append(filterChainMatch.ApplicationProtocols, mtlsHTTPALPNs...)
			}
			httpOpts = configgen.buildSidecarInboundHTTPListenerOptsForPortOrUDS(node, pluginParams)

		case plugin.ListenerProtocolThrift:
			filterChainMatch = chain.FilterChainMatch
			thriftOpts = configgen.buildSidecarThriftListenerOptsForPortOrUDS(pluginParams)

		case plugin.ListenerProtocolTCP:
			filterChainMatch = chain.FilterChainMatch
			tcpNetworkFilters = buildInboundNetworkFilters(pluginParams.Push, pluginParams.Node, pluginParams.ServiceInstance)

		case plugin.ListenerProtocolAuto:
			// Make sure id is not out of boundary of filterChainMatchOption
			if filterChainMatchOption == nil || len(filterChainMatchOption) <= id {
				continue
			}

			// TODO(yxue) avoid bypassing authN using TCP
			// Build filter chain options for listener configured with protocol sniffing
			fcm := listener.FilterChainMatch{}
			if chain.FilterChainMatch != nil {
				fcm = *chain.FilterChainMatch
			}
			fcm.ApplicationProtocols = filterChainMatchOption[id].ApplicationProtocols
			fcm.TransportProtocol = filterChainMatchOption[id].TransportProtocol
			filterChainMatch = &fcm
			if filterChainMatchOption[id].Protocol == plugin.ListenerProtocolHTTP {
				httpOpts = configgen.buildSidecarInboundHTTPListenerOptsForPortOrUDS(node, pluginParams)
			} else {
				tcpNetworkFilters = buildInboundNetworkFilters(pluginParams.Push, pluginParams.Node, pluginParams.ServiceInstance)
			}
		default:
			log.Warnf("Unsupported inbound protocol %v for port %#v", pluginParams.ListenerProtocol,
				pluginParams.ServiceInstance.ServicePort)
			return nil
		}

		listenerOpts.filterChainOpts = append(listenerOpts.filterChainOpts, &filterChainOpts{
			httpOpts:        httpOpts,
			thriftOpts:      thriftOpts,
			networkFilters:  tcpNetworkFilters,
			tlsContext:      chain.TLSContext,
			match:           filterChainMatch,
			listenerFilters: chain.ListenerFilters,
		})
	}

	// call plugins
	l := buildListener(listenerOpts)
	l.TrafficDirection = core.TrafficDirection_INBOUND

	mutable := &plugin.MutableObjects{
		Listener:     l,
		FilterChains: getPluginFilterChain(listenerOpts),
	}
	for _, p := range configgen.Plugins {
		if err := p.OnInboundListener(pluginParams, mutable); err != nil {
			log.Warn(err.Error())
		}
	}
	// Filters are serialized one time into an opaque struct once we have the complete list.
	if err := buildCompleteFilterChain(pluginParams, mutable, listenerOpts); err != nil {
		log.Warna("buildSidecarInboundListeners ", err.Error())
		return nil
	}

	listenerMap[listenerMapKey] = &inboundListenerEntry{
		bind:             listenerOpts.bind,
		instanceHostname: pluginParams.ServiceInstance.Service.Hostname,
	}
	return mutable.Listener
}

type inboundListenerEntry struct {
	bind             string
	instanceHostname host.Name // could be empty if generated via Sidecar CRD
}

type outboundListenerEntry struct {
	services    []*model.Service
	servicePort *model.Port
	bind        string
	listener    *xdsapi.Listener
	locked      bool
	protocol    protocol.Instance
}

func protocolName(node *model.Proxy, p protocol.Instance) string {
	switch plugin.ModelProtocolToListenerProtocol(node, p, core.TrafficDirection_OUTBOUND) {
	case plugin.ListenerProtocolHTTP:
		return "HTTP"
	case plugin.ListenerProtocolTCP:
		return "TCP"
	default:
		return "UNKNOWN"
	}
}

type outboundListenerConflict struct {
	metric          monitoring.Metric
	node            *model.Proxy
	listenerName    string
	currentProtocol protocol.Instance
	currentServices []*model.Service
	newHostname     host.Name
	newProtocol     protocol.Instance
}

func (c outboundListenerConflict) addMetric(node *model.Proxy, metrics model.Metrics) {
	currentHostnames := make([]string, len(c.currentServices))
	for i, s := range c.currentServices {
		currentHostnames[i] = string(s.Hostname)
	}
	concatHostnames := strings.Join(currentHostnames, ",")
	metrics.AddMetric(c.metric,
		c.listenerName,
		c.node,
		fmt.Sprintf("Listener=%s Accepted%s=%s Rejected%s=%s %sServices=%d",
			c.listenerName,
			protocolName(node, c.currentProtocol),
			concatHostnames,
			protocolName(node, c.newProtocol),
			c.newHostname,
			protocolName(node, c.currentProtocol),
			len(c.currentServices)))
}

// buildSidecarOutboundListeners generates http and tcp listeners for
// outbound connections from the proxy based on the sidecar scope associated with the proxy.
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundListeners(node *model.Proxy,
	push *model.PushContext) []*xdsapi.Listener {

	noneMode := node.GetInterceptionMode() == model.InterceptionNone

	actualWildcard, actualLocalHostAddress := getActualWildcardAndLocalHost(node)

	var tcpListeners, httpListeners []*xdsapi.Listener
	// For conflict resolution
	listenerMap := make(map[string]*outboundListenerEntry)

	// The sidecarConfig if provided could filter the list of
	// services/virtual services that we need to process. It could also
	// define one or more listeners with specific ports. Once we generate
	// listeners for these user specified ports, we will auto generate
	// configs for other ports if and only if the sidecarConfig has an
	// egressListener on wildcard port.
	//
	// Validation will ensure that we have utmost one wildcard egress listener
	// occurring in the end

	// Add listeners based on the config in the sidecar.EgressListeners if
	// no Sidecar CRD is provided for this config namespace,
	// push.SidecarScope will generate a default catch all egress listener.
	for _, egressListener := range node.SidecarScope.EgressListeners {

		services := egressListener.Services()
		virtualServices := egressListener.VirtualServices()

		// determine the bindToPort setting for listeners
		bindToPort := false
		if noneMode {
			// do not care what the listener's capture mode setting is. The proxy does not use iptables
			bindToPort = true
		} else if egressListener.IstioListener != nil &&
			// proxy uses iptables redirect or tproxy. IF mode is not set
			// for older proxies, it defaults to iptables redirect.  If the
			// listener's capture mode specifies NONE, then the proxy wants
			// this listener alone to be on a physical port. If the
			// listener's capture mode is default, then its same as
			// iptables i.e. bindToPort is false.
			egressListener.IstioListener.CaptureMode == networking.CaptureMode_NONE {
			bindToPort = true
		}

		if egressListener.IstioListener != nil &&
			egressListener.IstioListener.Port != nil {
			// We have a non catch all listener on some user specified port
			// The user specified port may or may not match a service port.
			// If it does not match any service port and the service has only
			// one port, then we pick a default service port. If service has
			// multiple ports, we expect the user to provide a virtualService
			// that will route to a proper Service.

			listenPort := &model.Port{
				Port:     int(egressListener.IstioListener.Port.Number),
				Protocol: protocol.Parse(egressListener.IstioListener.Port.Protocol),
				Name:     egressListener.IstioListener.Port.Name,
			}

			// If capture mode is NONE i.e., bindToPort is true, and
			// Bind IP + Port is specified, we will bind to the specified IP and Port.
			// This specified IP is ideally expected to be a loopback IP.
			//
			// If capture mode is NONE i.e., bindToPort is true, and
			// only Port is specified, we will bind to the default loopback IP
			// 127.0.0.1 and the specified Port.
			//
			// If capture mode is NONE, i.e., bindToPort is true, and
			// only Bind IP is specified, we will bind to the specified IP
			// for each port as defined in the service registry.
			//
			// If captureMode is not NONE, i.e., bindToPort is false, then
			// we will bind to user specified IP (if any) or to the VIPs of services in
			// this egress listener.
			bind := egressListener.IstioListener.Bind
			if bindToPort && bind == "" {
				bind = actualLocalHostAddress
			} else if len(bind) == 0 {
				bind = actualWildcard
			}

			for _, service := range services {
				listenerOpts := buildListenerOpts{
					push:       push,
					proxy:      node,
					bind:       bind,
					port:       listenPort.Port,
					bindToPort: bindToPort,
				}

				// The listener protocol is determined by the protocol of egress listener port.
				pluginParams := &plugin.InputParams{
					ListenerProtocol: plugin.ModelProtocolToListenerProtocol(node, listenPort.Protocol,
						core.TrafficDirection_OUTBOUND),
					DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
					Node:                       node,
					Push:                       push,
					Bind:                       bind,
					Port:                       listenPort,
					Service:                    service,
				}

				configgen.buildSidecarOutboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap,
					virtualServices, actualWildcard)
			}
		} else {
			// This is a catch all egress listener with no port. This
			// should be the last egress listener in the sidecar
			// Scope. Construct a listener for each service and service
			// port, if and only if this port was not specified in any of
			// the preceding listeners from the sidecarScope. This allows
			// users to specify a trimmed set of services for one or more
			// listeners and then add a catch all egress listener for all
			// other ports. Doing so allows people to restrict the set of
			// services exposed on one or more listeners, and avoid hard
			// port conflicts like tcp taking over http or http taking over
			// tcp, or simply specify that of all the listeners that Istio
			// generates, the user would like to have only specific sets of
			// services exposed on a particular listener.
			//
			// To ensure that we do not add anything to listeners we have
			// already generated, run through the outboundListenerEntry map and set
			// the locked bit to true.
			// buildSidecarOutboundListenerForPortOrUDS will not add/merge
			// any HTTP/TCP listener if there is already a outboundListenerEntry
			// with locked bit set to true
			for _, e := range listenerMap {
				e.locked = true
			}

			bind := ""
			if egressListener.IstioListener != nil && egressListener.IstioListener.Bind != "" {
				bind = egressListener.IstioListener.Bind
			}
			if bindToPort && bind == "" {
				bind = actualLocalHostAddress
			}
			for _, service := range services {
				for _, servicePort := range service.Ports {
					listenerOpts := buildListenerOpts{
						push:       push,
						proxy:      node,
						port:       servicePort.Port,
						bind:       bind,
						bindToPort: bindToPort,
					}

					// The listener protocol is determined by the protocol of service port.
					pluginParams := &plugin.InputParams{
						ListenerProtocol: plugin.ModelProtocolToListenerProtocol(node, servicePort.Protocol,
							core.TrafficDirection_OUTBOUND),
						DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
						Node:                       node,
						Push:                       push,
						Bind:                       bind,
						Port:                       servicePort,
						Service:                    service,
					}

					// Support Kubernetes statefulsets/headless services with TCP ports only.
					// Instead of generating a single 0.0.0.0:Port listener, generate a listener
					// for each instance. HTTP services can happily reside on 0.0.0.0:PORT and use the
					// wildcard route match to get to the appropriate pod through original dst clusters.
					if features.EnableHeadlessService.Get() && bind == "" && service.Resolution == model.Passthrough &&
						service.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) && servicePort.Protocol.IsTCP() {
						if instances, err := push.InstancesByPort(service, servicePort.Port, nil); err == nil {
							for _, instance := range instances {
								// Skip build outbound listener to the node itself,
								// as when app access itself by pod ip will not flow through this listener.
								// Simultaneously, it will be duplicate with inbound listener.
								if instance.Endpoint.Address == node.IPAddresses[0] {
									continue
								}
								listenerOpts.bind = instance.Endpoint.Address
								configgen.buildSidecarOutboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap,
									virtualServices, actualWildcard)
							}
						} else {
							// we can't do anything. Fallback to the usual way of constructing listeners
							// for headless services that use 0.0.0.0:Port listener
							configgen.buildSidecarOutboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap,
								virtualServices, actualWildcard)
						}
					} else {
						// Standard logic for headless and non headless services
						if features.EnableThriftFilter.Get() &&
							servicePort.Protocol.IsThrift() {
							listenerOpts.bind = service.Address
						}
						configgen.buildSidecarOutboundListenerForPortOrUDS(node, listenerOpts, pluginParams, listenerMap,
							virtualServices, actualWildcard)
					}
				}
			}
		}
	}

	// Now validate all the listeners. Collate the tcp listeners first and then the HTTP listeners
	// TODO: This is going to be bad for caching as the order of listeners in tcpListeners or httpListeners is not
	// guaranteed.
	invalid := 0.0
	for name, l := range listenerMap {
		if err := l.listener.Validate(); err != nil {
			log.Warnf("buildSidecarOutboundListeners: error validating listener %s (type %v): %v", name, l.servicePort.Protocol, err)
			invalid++
			invalidOutboundListeners.Record(invalid)
			continue
		}
		if l.servicePort.Protocol.IsTCP() {
			tcpListeners = append(tcpListeners, l.listener)
		} else {
			httpListeners = append(httpListeners, l.listener)
		}
	}

	tcpListeners = append(tcpListeners, httpListeners...)
	httpProxy := configgen.buildHTTPProxy(node, push)
	if httpProxy != nil {
		tcpListeners = append(tcpListeners, httpProxy)
	}

	removeListenerFilterTimeout(tcpListeners)
	return tcpListeners
}

func (configgen *ConfigGeneratorImpl) buildHTTPProxy(node *model.Proxy,
	push *model.PushContext) *xdsapi.Listener {
	httpProxyPort := push.Mesh.ProxyHttpPort
	if httpProxyPort == 0 {
		return nil
	}

	// enable HTTP PROXY port if necessary; this will add an RDS route for this port
	_, listenAddress := getActualWildcardAndLocalHost(node)

	httpOpts := &core.Http1ProtocolOptions{
		AllowAbsoluteUrl: proto.BoolTrue,
	}
	if features.HTTP10 || node.Metadata.HTTP10 == "1" {
		httpOpts.AcceptHttp_10 = true
	}

	opts := buildListenerOpts{
		push:  push,
		proxy: node,
		bind:  listenAddress,
		port:  int(httpProxyPort),
		filterChainOpts: []*filterChainOpts{{
			httpOpts: &httpListenerOpts{
				rds:              RDSHttpProxy,
				useRemoteAddress: false,
				connectionManager: &http_conn.HttpConnectionManager{
					HttpProtocolOptions: httpOpts,
				},
			},
		}},
		bindToPort:      true,
		skipUserFilters: true,
	}
	l := buildListener(opts)
	l.TrafficDirection = core.TrafficDirection_OUTBOUND

	// TODO: plugins for HTTP_PROXY mode, envoyfilter needs another listener match for SIDECAR_HTTP_PROXY
	// there is no mixer for http_proxy
	mutable := &plugin.MutableObjects{
		Listener:     l,
		FilterChains: []plugin.FilterChain{{}},
	}
	pluginParams := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
		Node:             node,
		Push:             push,
	}
	if err := buildCompleteFilterChain(pluginParams, mutable, opts); err != nil {
		log.Warna("buildHTTPProxy filter chain error  ", err.Error())
		return nil
	}
	return l
}

func (configgen *ConfigGeneratorImpl) buildSidecarOutboundHTTPListenerOptsForPortOrUDS(node *model.Proxy, listenerMapKey *string,
	currentListenerEntry **outboundListenerEntry, listenerOpts *buildListenerOpts,
	pluginParams *plugin.InputParams, listenerMap map[string]*outboundListenerEntry, actualWildcard string) (bool, []*filterChainOpts) {
	// first identify the bind if its not set. Then construct the key
	// used to lookup the listener in the conflict map.
	if len(listenerOpts.bind) == 0 { // no user specified bind. Use 0.0.0.0:Port
		listenerOpts.bind = actualWildcard
	}
	*listenerMapKey = listenerOpts.bind + ":" + strconv.Itoa(pluginParams.Port.Port)

	var exists bool

	// Have we already generated a listener for this Port based on user
	// specified listener ports? if so, we should not add any more HTTP
	// services to the port. The user could have specified a sidecar
	// resource with one or more explicit ports and then added a catch
	// all listener, implying add all other ports as usual. When we are
	// iterating through the services for a catchAll egress listener,
	// the caller would have set the locked bit for each listener Entry
	// in the map.
	//
	// Check if this HTTP listener conflicts with an existing TCP
	// listener. We could have listener conflicts occur on unix domain
	// sockets, or on IP binds. Specifically, its common to see
	// conflicts on binds for wildcard address when a service has NONE
	// resolution type, since we collapse all HTTP listeners into a
	// single 0.0.0.0:port listener and use vhosts to distinguish
	// individual http services in that port
	if *currentListenerEntry, exists = listenerMap[*listenerMapKey]; exists {
		// NOTE: This is not a conflict. This is simply filtering the
		// services for a given listener explicitly.
		// When the user declares their own ports in Sidecar.egress
		// with some specific services on those ports, we should not
		// generate any more listeners on that port as the user does
		// not want those listeners. Protocol sniffing is not needed.
		if (*currentListenerEntry).locked {
			return false, nil
		}

		if !util.IsProtocolSniffingEnabledForOutbound(node) {
			if pluginParams.Service != nil {
				if !(*currentListenerEntry).servicePort.Protocol.IsHTTP() {
					outboundListenerConflict{
						metric:          model.ProxyStatusConflictOutboundListenerTCPOverHTTP,
						node:            pluginParams.Node,
						listenerName:    *listenerMapKey,
						currentServices: (*currentListenerEntry).services,
						currentProtocol: (*currentListenerEntry).servicePort.Protocol,
						newHostname:     pluginParams.Service.Hostname,
						newProtocol:     pluginParams.Port.Protocol,
					}.addMetric(node, pluginParams.Push)
				}

				// Skip building listener for the same http port
				(*currentListenerEntry).services = append((*currentListenerEntry).services, pluginParams.Service)
			}
			return false, nil
		}
	}

	// No conflicts. Add a http filter chain option to the listenerOpts
	var rdsName string
	if pluginParams.Port.Port == 0 {
		rdsName = listenerOpts.bind // use the UDS as a rds name
	} else {
		if pluginParams.ListenerProtocol == plugin.ListenerProtocolAuto &&
			util.IsProtocolSniffingEnabledForOutbound(node) && listenerOpts.bind != actualWildcard && pluginParams.Service != nil {
			rdsName = string(pluginParams.Service.Hostname) + ":" + strconv.Itoa(pluginParams.Port.Port)
		} else {
			rdsName = strconv.Itoa(pluginParams.Port.Port)
		}
	}
	httpOpts := &httpListenerOpts{
		// Set useRemoteAddress to true for side car outbound listeners so that it picks up the localhost address of the sender,
		// which is an internal address, so that trusted headers are not sanitized. This helps to retain the timeout headers
		// such as "x-envoy-upstream-rq-timeout-ms" set by the calling application.
		useRemoteAddress: features.UseRemoteAddress.Get(),
		rds:              rdsName,
	}

	if features.HTTP10 || pluginParams.Node.Metadata.HTTP10 == "1" {
		httpOpts.connectionManager = &http_conn.HttpConnectionManager{
			HttpProtocolOptions: &core.Http1ProtocolOptions{
				AcceptHttp_10: true,
			},
		}
	}

	return true, []*filterChainOpts{{
		httpOpts: httpOpts,
	}}
}

func (configgen *ConfigGeneratorImpl) buildSidecarOutboundThriftListenerOptsForPortOrUDS(_ *model.Proxy, listenerMapKey *string,
	currentListenerEntry **outboundListenerEntry, listenerOpts *buildListenerOpts,
	pluginParams *plugin.InputParams, listenerMap map[string]*outboundListenerEntry, actualWildcard string) (bool, []*filterChainOpts) {
	// first identify the bind if its not set. Then construct the key
	// used to lookup the listener in the conflict map.
	if len(listenerOpts.bind) == 0 { // no user specified bind. Use 0.0.0.0:Port
		listenerOpts.bind = actualWildcard
	}
	*listenerMapKey = fmt.Sprintf("%s:%d", listenerOpts.bind, pluginParams.Port.Port)

	var exists bool

	// Have we already generated a listener for this Port based on user
	// specified listener ports? if so, we should not add any more Thrift
	// services to the port. The user could have specified a sidecar
	// resource with one or more explicit ports and then added a catch
	// all listener, implying add all other ports as usual. When we are
	// iterating through the services for a catchAll egress listener,
	// the caller would have set the locked bit for each listener Entry
	// in the map.
	//
	// Check if this Thrift listener conflicts with an existing TCP or
	// HTTP listener. We could have listener conflicts occur on unix
	// domain sockets, or on IP binds.
	if *currentListenerEntry, exists = listenerMap[*listenerMapKey]; exists {
		// NOTE: This is not a conflict. This is simply filtering the
		// services for a given listener explicitly.
		// When the user declares their own ports in Sidecar.egress
		// with some specific services on those ports, we should not
		// generate any more listeners on that port as the user does
		// not want those listeners. Protocol sniffing is not needed.
		if (*currentListenerEntry).locked {
			return false, nil
		}
	}

	// No conflicts. Add a thrift filter chain option to the listenerOpts
	clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", pluginParams.Service.Hostname, pluginParams.Port.Port)
	thriftOpts := &thriftListenerOpts{
		protocol:    thrift_proxy.ProtocolType_AUTO_PROTOCOL,
		transport:   thrift_proxy.TransportType_AUTO_TRANSPORT,
		routeConfig: configgen.buildSidecarThriftRouteConfig(clusterName, pluginParams.Push.Mesh.ThriftConfig.RateLimitUrl),
	}

	return true, []*filterChainOpts{{
		thriftOpts: thriftOpts,
	}}
}

func (configgen *ConfigGeneratorImpl) buildSidecarOutboundTCPListenerOptsForPortOrUDS(node *model.Proxy, destinationCIDR *string, listenerMapKey *string,
	currentListenerEntry **outboundListenerEntry, listenerOpts *buildListenerOpts,
	pluginParams *plugin.InputParams, listenerMap map[string]*outboundListenerEntry,
	virtualServices []model.Config, actualWildcard string) (bool, []*filterChainOpts) {

	// first identify the bind if its not set. Then construct the key
	// used to lookup the listener in the conflict map.

	// Determine the listener address if bind is empty
	// we listen on the service VIP if and only
	// if the address is an IP address. If its a CIDR, we listen on
	// 0.0.0.0, and setup a filter chain match for the CIDR range.
	// As a small optimization, CIDRs with /32 prefix will be converted
	// into listener address so that there is a dedicated listener for this
	// ip:port. This will reduce the impact of a listener reload

	if len(listenerOpts.bind) == 0 {
		svcListenAddress := pluginParams.Service.GetServiceAddressForProxy(pluginParams.Node)
		// We should never get an empty address.
		// This is a safety guard, in case some platform adapter isn't doing things
		// properly
		if len(svcListenAddress) > 0 {
			if !strings.Contains(svcListenAddress, "/") {
				listenerOpts.bind = svcListenAddress
			} else {
				// Address is a CIDR. Fall back to 0.0.0.0 and
				// filter chain match
				*destinationCIDR = svcListenAddress
				listenerOpts.bind = actualWildcard
			}
		}
	}

	// could be a unix domain socket or an IP bind
	*listenerMapKey = fmt.Sprintf("%s:%d", listenerOpts.bind, pluginParams.Port.Port)

	var exists bool

	// Have we already generated a listener for this Port based on user
	// specified listener ports? if so, we should not add any more
	// services to the port. The user could have specified a sidecar
	// resource with one or more explicit ports and then added a catch
	// all listener, implying add all other ports as usual. When we are
	// iterating through the services for a catchAll egress listener,
	// the caller would have set the locked bit for each listener Entry
	// in the map.
	//
	// Check if this TCP listener conflicts with an existing HTTP listener
	if *currentListenerEntry, exists = listenerMap[*listenerMapKey]; exists {
		// NOTE: This is not a conflict. This is simply filtering the
		// services for a given listener explicitly.
		// When the user declares their own ports in Sidecar.egress
		// with some specific services on those ports, we should not
		// generate any more listeners on that port as the user does
		// not want those listeners. Protocol sniffing is not needed.
		if (*currentListenerEntry).locked {
			return false, nil
		}

		if !util.IsProtocolSniffingEnabledForOutbound(node) {
			// Check for port collisions between TCP/TLS and HTTP (or unknown). If
			// configured correctly, TCP/TLS ports may not collide. We'll
			// need to do additional work to find out if there is a
			// collision within TCP/TLS.
			// If the service port was defined as unknown. It will conflict with all other
			// protocols.
			if !(*currentListenerEntry).servicePort.Protocol.IsTCP() {
				// NOTE: While pluginParams.Service can be nil,
				// this code cannot be reached if Service is nil because a pluginParams.Service can be nil only
				// for user defined Egress listeners with ports. And these should occur in the API before
				// the wildcard egress listener. the check for the "locked" bit will eliminate the collision.
				// User is also not allowed to add duplicate ports in the egress listener
				var newHostname host.Name
				if pluginParams.Service != nil {
					newHostname = pluginParams.Service.Hostname
				} else {
					// user defined outbound listener via sidecar API
					newHostname = "sidecar-config-egress-http-listener"
				}

				outboundListenerConflict{
					metric:          model.ProxyStatusConflictOutboundListenerHTTPOverTCP,
					node:            pluginParams.Node,
					listenerName:    *listenerMapKey,
					currentServices: (*currentListenerEntry).services,
					currentProtocol: (*currentListenerEntry).servicePort.Protocol,
					newHostname:     newHostname,
					newProtocol:     pluginParams.Port.Protocol,
				}.addMetric(node, pluginParams.Push)
				return false, nil
			}

			// We have a collision with another TCP port. This can happen
			// for headless services, or non-k8s services that do not have
			// a VIP, or when we have two binds on a unix domain socket or
			// on same IP.  Unfortunately we won't know if this is a real
			// conflict or not until we process the VirtualServices, etc.
			// The conflict resolution is done later in this code
		}
	}

	meshGateway := map[string]bool{constants.IstioMeshGateway: true}
	return true, buildSidecarOutboundTCPTLSFilterChainOpts(pluginParams.Node,
		pluginParams.Push, virtualServices,
		*destinationCIDR, pluginParams.Service,
		pluginParams.Port, meshGateway)
}

// buildSidecarOutboundListenerForPortOrUDS builds a single listener and
// adds it to the listenerMap provided by the caller.  Listeners are added
// if one doesn't already exist. HTTP listeners on same port are ignored
// (as vhosts are shipped through RDS).  TCP listeners on same port are
// allowed only if they have different CIDR matches.
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundListenerForPortOrUDS(node *model.Proxy, listenerOpts buildListenerOpts,
	pluginParams *plugin.InputParams, listenerMap map[string]*outboundListenerEntry,
	virtualServices []model.Config, actualWildcard string) {
	if features.BlockHTTPonHTTPSPort {
		if listenerOpts.port == CanonicalHTTPSPort && pluginParams.Port.Protocol == protocol.HTTP {
			msg := fmt.Sprintf("listener conflict detected: service %v specifies an HTTP service on HTTPS only port %d.",
				pluginParams.Service.Hostname, CanonicalHTTPSPort)
			pluginParams.Push.AddMetric(model.ProxyStatusConflictOutboundListenerHTTPoverHTTPS, string(pluginParams.Service.Hostname), node, msg)
			return
		}
	}
	var destinationCIDR string
	var listenerMapKey string
	var currentListenerEntry *outboundListenerEntry
	var ret bool
	var opts []*filterChainOpts

	conflictType := NoConflict

	// For HTTP_PROXY protocol defined by sidecars, just create the HTTP listener right away.
	if pluginParams.Port.Protocol == protocol.HTTP_PROXY {
		if ret, opts = configgen.buildSidecarOutboundHTTPListenerOptsForPortOrUDS(node, &listenerMapKey, &currentListenerEntry,
			&listenerOpts, pluginParams, listenerMap, actualWildcard); !ret {
			return
		}
		listenerOpts.filterChainOpts = opts
	} else {
		switch pluginParams.ListenerProtocol {
		case plugin.ListenerProtocolHTTP:
			if ret, opts = configgen.buildSidecarOutboundHTTPListenerOptsForPortOrUDS(node, &listenerMapKey, &currentListenerEntry,
				&listenerOpts, pluginParams, listenerMap, actualWildcard); !ret {
				return
			}

			// Check if conflict happens
			if util.IsProtocolSniffingEnabledForOutbound(node) && currentListenerEntry != nil {
				// Build HTTP listener. If current listener entry is using HTTP or protocol sniffing,
				// append the service. Otherwise (TCP), change current listener to use protocol sniffing.
				if currentListenerEntry.protocol.IsHTTP() {
					conflictType = HTTPOverHTTP
				} else if currentListenerEntry.protocol.IsTCP() {
					conflictType = HTTPOverTCP
				} else {
					conflictType = HTTPOverAuto
				}
			}

			listenerOpts.filterChainOpts = opts

		case plugin.ListenerProtocolThrift:
			// Hard code the service IP for outbound thrift service listeners. HTTP services
			// use RDS but the Thrift stack has no such dynamic configuration option.
			if ret, opts = configgen.buildSidecarOutboundThriftListenerOptsForPortOrUDS(node, &listenerMapKey, &currentListenerEntry,
				&listenerOpts, pluginParams, listenerMap, actualWildcard); !ret {
				return
			}

			// Protocol sniffing for thrift is not supported.
			if util.IsProtocolSniffingEnabledForOutbound(node) && currentListenerEntry != nil {
				// We should not ever end up here, but log a line just in case.
				log.Errorf(
					"Protocol sniffing is not enabled for thrift, but there was a port collision. Debug info: Node: %v, ListenerEntry: %v",
					node,
					currentListenerEntry)
			}

			listenerOpts.filterChainOpts = opts

		case plugin.ListenerProtocolTCP:
			if ret, opts = configgen.buildSidecarOutboundTCPListenerOptsForPortOrUDS(node, &destinationCIDR, &listenerMapKey, &currentListenerEntry,
				&listenerOpts, pluginParams, listenerMap, virtualServices, actualWildcard); !ret {
				return
			}

			// Check if conflict happens
			if util.IsProtocolSniffingEnabledForOutbound(node) && currentListenerEntry != nil {
				// Build TCP listener. If current listener entry is using HTTP, add a new TCP filter chain
				// If current listener is using protocol sniffing, merge the TCP filter chains.
				if currentListenerEntry.protocol.IsHTTP() {
					conflictType = TCPOverHTTP
				} else if currentListenerEntry.protocol.IsTCP() {
					conflictType = TCPOverTCP
				} else {
					conflictType = TCPOverAuto
				}
			}

			listenerOpts.filterChainOpts = opts

		case plugin.ListenerProtocolAuto:
			// Add tcp filter chain, build TCP filter chain first.
			if ret, opts = configgen.buildSidecarOutboundTCPListenerOptsForPortOrUDS(node, &destinationCIDR, &listenerMapKey, &currentListenerEntry,
				&listenerOpts, pluginParams, listenerMap, virtualServices, actualWildcard); !ret {
				return
			}
			listenerOpts.filterChainOpts = append(listenerOpts.filterChainOpts, opts...)

			// Add http filter chain and tcp filter chain to the listener opts
			if ret, opts = configgen.buildSidecarOutboundHTTPListenerOptsForPortOrUDS(node, &listenerMapKey, &currentListenerEntry,
				&listenerOpts, pluginParams, listenerMap, actualWildcard); !ret {
				return
			}

			// Add application protocol filter chain match to the http filter chain. The application protocol will be set by http inspector
			for _, opt := range opts {
				if opt.match == nil {
					opt.match = &listener.FilterChainMatch{}
				}

				// Support HTTP/1.0, HTTP/1.1 and HTTP/2
				opt.match.ApplicationProtocols = append(opt.match.ApplicationProtocols, plaintextHTTPALPNs...)
			}

			listenerOpts.filterChainOpts = append(listenerOpts.filterChainOpts, opts...)
			listenerOpts.needHTTPInspector = true

			if currentListenerEntry != nil {
				if currentListenerEntry.protocol.IsHTTP() {
					conflictType = AutoOverHTTP
				} else if currentListenerEntry.protocol.IsTCP() {
					conflictType = AutoOverTCP
				} else {
					conflictType = AutoOverAuto
				}
			}

		default:
			// UDP or other protocols: no need to log, it's too noisy
			return
		}
	}

	// These wildcard listeners are intended for outbound traffic. However, there are cases where inbound traffic can hit these.
	// This will happen when there is a no more specific inbound listener, either because Pilot hasn't sent it (race condition
	// at startup), or because it never will (a port not specified in a service but captured by iptables).
	// When this happens, Envoy will infinite loop sending requests to itself.
	// To prevent this, we add a filter chain match that will match the pod ip and blackhole the traffic.
	if listenerOpts.bind == actualWildcard && features.RestrictPodIPTrafficLoops.Get() {
		listenerOpts.filterChainOpts = append([]*filterChainOpts{{
			destinationCIDRs: pluginParams.Node.IPAddresses,
			networkFilters:   []*listener.Filter{blackholeFilter},
		}}, listenerOpts.filterChainOpts...)
	}

	// Lets build the new listener with the filter chains. In the end, we will
	// merge the filter chains with any existing listener on the same port/bind point
	l := buildListener(listenerOpts)
	appendListenerFallthroughRoute(l, &listenerOpts, pluginParams.Node, currentListenerEntry)
	l.TrafficDirection = core.TrafficDirection_OUTBOUND

	mutable := &plugin.MutableObjects{
		Listener:     l,
		FilterChains: getPluginFilterChain(listenerOpts),
	}

	for _, p := range configgen.Plugins {
		if err := p.OnOutboundListener(pluginParams, mutable); err != nil {
			log.Warn(err.Error())
		}
	}

	// Filters are serialized one time into an opaque struct once we have the complete list.
	if err := buildCompleteFilterChain(pluginParams, mutable, listenerOpts); err != nil {
		log.Warna("buildSidecarOutboundListeners: ", err.Error())
		return
	}

	// If there is a TCP listener on well known port, cannot add any http filter chain
	// with the inspector as it will break for server-first protocols. Similarly,
	// if there was a HTTP listener on well known port, cannot add a tcp listener
	// with the inspector as inspector breaks all server-first protocols.
	if currentListenerEntry != nil &&
		!isConflictWithWellKnownPort(pluginParams.Port.Protocol, currentListenerEntry.protocol, conflictType) {
		log.Warnf("conflict happens on a well known port %d, incoming protocol %v, existing protocol %v, conflict type %v",
			pluginParams.Port.Port, pluginParams.Port.Protocol, currentListenerEntry.protocol, conflictType)
		return
	}

	// There are 9 types conflicts
	//    Incoming Existing
	//  1. HTTP -> HTTP
	//  2. HTTP -> TCP
	//  3. HTTP -> unknown
	//  4. TCP  -> HTTP
	//  5. TCP  -> TCP
	//  6. TCP  -> unknown
	//  7. unknown -> HTTP
	//  8. unknown -> TCP
	//  9. unknown -> unknown
	//  Type 1 can be resolved by appending service to existing services
	//  Type 2 can be resolved by merging TCP filter chain with HTTP filter chain
	//  Type 3 can be resolved by appending service to existing services
	//  Type 4 can be resolved by merging HTTP filter chain with TCP filter chain
	//  Type 5 can be resolved by merging TCP filter chains
	//  Type 6 can be resolved by merging TCP filter chains
	//  Type 7 can be resolved by appending service to existing services
	//  Type 8 can be resolved by merging TCP filter chains
	//  Type 9 can be resolved by merging TCP and HTTP filter chains

	switch conflictType {
	case NoConflict:
		if currentListenerEntry != nil {
			currentListenerEntry.listener.FilterChains = mergeTCPFilterChains(mutable.Listener.FilterChains,
				pluginParams, listenerMapKey, listenerMap, node)
		} else {
			listenerMap[listenerMapKey] = &outboundListenerEntry{
				services:    []*model.Service{pluginParams.Service},
				servicePort: pluginParams.Port,
				bind:        listenerOpts.bind,
				listener:    mutable.Listener,
				protocol:    pluginParams.Port.Protocol,
			}
		}

	case HTTPOverHTTP, HTTPOverAuto:
		// Append service only
		currentListenerEntry.services = append(currentListenerEntry.services, pluginParams.Service)
	case HTTPOverTCP:
		// Merge HTTP filter chain to TCP filter chain
		currentListenerEntry.listener.FilterChains = mergeFilterChains(mutable.Listener.FilterChains, currentListenerEntry.listener.FilterChains)
		currentListenerEntry.protocol = protocol.Unsupported
		currentListenerEntry.listener.ListenerFilters = appendListenerFilters(currentListenerEntry.listener.ListenerFilters)
		currentListenerEntry.services = append(currentListenerEntry.services, pluginParams.Service)

	case TCPOverHTTP:
		// Merge TCP filter chain to HTTP filter chain
		currentListenerEntry.listener.FilterChains = mergeFilterChains(currentListenerEntry.listener.FilterChains, mutable.Listener.FilterChains)
		currentListenerEntry.protocol = protocol.Unsupported
		currentListenerEntry.listener.ListenerFilters = appendListenerFilters(currentListenerEntry.listener.ListenerFilters)
	case TCPOverTCP:
		// Merge two TCP filter chains. HTTP filter chain will not conflict with TCP filter chain because HTTP filter chain match for
		// HTTP filter chain is different from TCP filter chain's.
		currentListenerEntry.listener.FilterChains = mergeTCPFilterChains(mutable.Listener.FilterChains,
			pluginParams, listenerMapKey, listenerMap, node)
	case TCPOverAuto:
		// Merge two TCP filter chains. HTTP filter chain will not conflict with TCP filter chain because HTTP filter chain match for
		// HTTP filter chain is different from TCP filter chain's.
		currentListenerEntry.listener.FilterChains = mergeTCPFilterChains(mutable.Listener.FilterChains,
			pluginParams, listenerMapKey, listenerMap, node)

	case AutoOverHTTP:
		listenerMap[listenerMapKey] = &outboundListenerEntry{
			services:    append(currentListenerEntry.services, pluginParams.Service),
			servicePort: pluginParams.Port,
			bind:        listenerOpts.bind,
			listener:    mutable.Listener,
			protocol:    protocol.Unsupported,
		}
		currentListenerEntry.listener.ListenerFilters = appendListenerFilters(currentListenerEntry.listener.ListenerFilters)

	case AutoOverTCP:
		// Merge two TCP filter chains. HTTP filter chain will not conflict with TCP filter chain because HTTP filter chain match for
		// HTTP filter chain is different from TCP filter chain's.
		currentListenerEntry.listener.FilterChains = mergeTCPFilterChains(mutable.Listener.FilterChains,
			pluginParams, listenerMapKey, listenerMap, node)
		currentListenerEntry.protocol = protocol.Unsupported
		currentListenerEntry.listener.ListenerFilters = appendListenerFilters(currentListenerEntry.listener.ListenerFilters)

	case AutoOverAuto:
		currentListenerEntry.services = append(currentListenerEntry.services, pluginParams.Service)
	}

	if log.DebugEnabled() && len(mutable.Listener.FilterChains) > 1 || currentListenerEntry != nil {
		var numChains int
		if currentListenerEntry != nil {
			numChains = len(currentListenerEntry.listener.FilterChains)
		} else {
			numChains = len(mutable.Listener.FilterChains)
		}
		log.Debugf("buildSidecarOutboundListeners: multiple filter chain listener %s with %d chains", mutable.Listener.Name, numChains)
	}
}

// onVirtualOutboundListener calls the plugin API for the outbound virtual listener
func (configgen *ConfigGeneratorImpl) onVirtualOutboundListener(
	node *model.Proxy,
	push *model.PushContext,
	ipTablesListener *xdsapi.Listener) *xdsapi.Listener {

	svc := util.FallThroughFilterChainBlackHoleService
	redirectPort := &model.Port{
		Port:     int(push.Mesh.ProxyListenPort),
		Protocol: protocol.TCP,
	}

	if len(ipTablesListener.FilterChains) < 1 || len(ipTablesListener.FilterChains[0].Filters) < 1 {
		return ipTablesListener
	}

	// contains all filter chains except for the final passthrough/blackhole
	initialFilterChain := ipTablesListener.FilterChains[:len(ipTablesListener.FilterChains)-1]

	// contains just the final passthrough/blackhole
	fallbackFilter := ipTablesListener.FilterChains[len(ipTablesListener.FilterChains)-1].Filters[0]

	if util.IsAllowAnyOutbound(node) {
		svc = util.FallThroughFilterChainPassthroughService
	}

	pluginParams := &plugin.InputParams{
		ListenerProtocol:           plugin.ListenerProtocolTCP,
		DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
		Node:                       node,
		Push:                       push,
		Bind:                       "",
		Port:                       redirectPort,
		Service:                    svc,
	}

	mutable := &plugin.MutableObjects{
		Listener:     ipTablesListener,
		FilterChains: make([]plugin.FilterChain, len(ipTablesListener.FilterChains)),
	}

	for _, p := range configgen.Plugins {
		if err := p.OnVirtualListener(pluginParams, mutable); err != nil {
			log.Warn(err.Error())
		}
	}
	if len(mutable.FilterChains) > 0 && len(mutable.FilterChains[0].TCP) > 0 {
		filters := append([]*listener.Filter{}, mutable.FilterChains[0].TCP...)
		filters = append(filters, fallbackFilter)

		// Replace the final filter chain with the new chain that has had plugins applied
		initialFilterChain = append(initialFilterChain, &listener.FilterChain{Filters: filters})
		ipTablesListener.FilterChains = initialFilterChain
	}
	return ipTablesListener
}

// buildSidecarInboundMgmtListeners creates inbound TCP only listeners for the management ports on
// server (inbound). Management port listeners are slightly different from standard Inbound listeners
// in that, they do not have mixer filters nor do they have inbound auth.
// N.B. If a given management port is same as the service instance's endpoint port
// the pod will fail to start in Kubernetes, because the mixer service tries to
// lookup the service associated with the Pod. Since the pod is yet to be started
// and hence not bound to the service), the service lookup fails causing the mixer
// to fail the health check call. This results in a vicious cycle, where kubernetes
// restarts the unhealthy pod after successive failed health checks, and the mixer
// continues to reject the health checks as there is no service associated with
// the pod.
// So, if a user wants to use kubernetes probes with Istio, she should ensure
// that the health check ports are distinct from the service ports.
func buildSidecarInboundMgmtListeners(node *model.Proxy, push *model.PushContext, managementPorts model.PortList, managementIP string) []*xdsapi.Listener {
	listeners := make([]*xdsapi.Listener, 0, len(managementPorts))
	// assumes that inbound connections/requests are sent to the endpoint address
	for _, mPort := range managementPorts {
		switch mPort.Protocol {
		case protocol.HTTP, protocol.HTTP2, protocol.GRPC, protocol.GRPCWeb, protocol.TCP,
			protocol.HTTPS, protocol.TLS, protocol.Mongo, protocol.Redis, protocol.MySQL:

			instance := &model.ServiceInstance{
				Service: &model.Service{
					Hostname: ManagementClusterHostname,
				},
				ServicePort: mPort,
				Endpoint: &model.IstioEndpoint{
					Address:      managementIP,
					EndpointPort: uint32(mPort.Port),
				},
			}
			listenerOpts := buildListenerOpts{
				bind: managementIP,
				port: mPort.Port,
				filterChainOpts: []*filterChainOpts{{
					networkFilters: buildInboundNetworkFilters(push, node, instance),
				}},
				// No user filters for the management unless we introduce new listener matches
				skipUserFilters: true,
				proxy:           node,
				push:            push,
			}
			l := buildListener(listenerOpts)
			l.TrafficDirection = core.TrafficDirection_INBOUND
			mutable := &plugin.MutableObjects{
				Listener:     l,
				FilterChains: []plugin.FilterChain{{}},
			}
			pluginParams := &plugin.InputParams{
				ListenerProtocol:           plugin.ListenerProtocolTCP,
				DeprecatedListenerCategory: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
				Push:                       push,
				Node:                       node,
				Port:                       mPort,
			}
			// TODO: should we call plugins for the admin port listeners too? We do everywhere else we construct listeners.
			if err := buildCompleteFilterChain(pluginParams, mutable, listenerOpts); err != nil {
				log.Warna("buildSidecarInboundMgmtListeners ", err.Error())
			} else {
				listeners = append(listeners, l)
			}
		default:
			log.Warnf("Unsupported inbound protocol %v for management port %#v",
				mPort.Protocol, mPort)
		}
	}

	return listeners
}

// httpListenerOpts are options for an HTTP listener
type httpListenerOpts struct {
	routeConfig *xdsapi.RouteConfiguration
	rds         string
	// If set, use this as a basis
	connectionManager *http_conn.HttpConnectionManager
	// stat prefix for the http connection manager
	// DO not set this field. Will be overridden by buildCompleteFilterChain
	statPrefix string
	// addGRPCWebFilter specifies whether the envoy.grpc_web HTTP filter
	// should be added.
	addGRPCWebFilter bool
	useRemoteAddress bool
}

// thriftListenerOpts are options for a Thrift listener
type thriftListenerOpts struct {
	// Stats are not provided for the Thrift filter chain
	transport   thrift_proxy.TransportType
	protocol    thrift_proxy.ProtocolType
	routeConfig *thrift_proxy.RouteConfiguration
}

// filterChainOpts describes a filter chain: a set of filters with the same TLS context
type filterChainOpts struct {
	sniHosts         []string
	destinationCIDRs []string
	metadata         *core.Metadata
	tlsContext       *auth.DownstreamTlsContext
	httpOpts         *httpListenerOpts
	thriftOpts       *thriftListenerOpts
	match            *listener.FilterChainMatch
	listenerFilters  []*listener.ListenerFilter
	networkFilters   []*listener.Filter
	isFallThrough    bool
}

// buildListenerOpts are the options required to build a Listener
type buildListenerOpts struct {
	// nolint: maligned
	push              *model.PushContext
	proxy             *model.Proxy
	bind              string
	port              int
	filterChainOpts   []*filterChainOpts
	bindToPort        bool
	skipUserFilters   bool
	needHTTPInspector bool
}

func buildHTTPConnectionManager(pluginParams *plugin.InputParams, httpOpts *httpListenerOpts,
	httpFilters []*http_conn.HttpFilter) *http_conn.HttpConnectionManager {
	filters := make([]*http_conn.HttpFilter, len(httpFilters))
	copy(filters, httpFilters)

	if httpOpts.addGRPCWebFilter {
		filters = append(filters, &http_conn.HttpFilter{Name: wellknown.GRPCWeb})
	}

	if util.IsIstioVersionGE14(pluginParams.Node) &&
		pluginParams.ServiceInstance != nil &&
		pluginParams.ServiceInstance.ServicePort != nil &&
		pluginParams.ServiceInstance.ServicePort.Protocol == protocol.GRPC {
		filters = append(filters, &http_conn.HttpFilter{
			Name: "envoy.filters.http.grpc_stats",
			ConfigType: &http_conn.HttpFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&grpc_stats.FilterConfig{
					EmitFilterState: true,
				}),
			},
		})
	}

	// append ALPN HTTP filter in HTTP connection manager for outbound listener only.
	if util.IsIstioVersionGE14(pluginParams.Node) &&
		(pluginParams.ListenerCategory == networking.EnvoyFilter_SIDECAR_OUTBOUND ||
			pluginParams.DeprecatedListenerCategory == networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND) {
		filters = append(filters, &http_conn.HttpFilter{
			Name: AlpnFilterName,
			ConfigType: &http_conn.HttpFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&alpn_filter.FilterConfig{
					AlpnOverride: []*alpn_filter.FilterConfig_AlpnOverride{
						{
							UpstreamProtocol: alpn_filter.FilterConfig_HTTP10,
							AlpnOverride:     mtlsHTTP10ALPN,
						},
						{
							UpstreamProtocol: alpn_filter.FilterConfig_HTTP11,
							AlpnOverride:     mtlsHTTP11ALPN,
						},
						{
							UpstreamProtocol: alpn_filter.FilterConfig_HTTP2,
							AlpnOverride:     mtlsHTTP2ALPN,
						},
					},
				}),
			},
		})
	}

	filters = append(filters,
		&http_conn.HttpFilter{Name: wellknown.CORS},
		&http_conn.HttpFilter{Name: wellknown.Fault},
		&http_conn.HttpFilter{Name: wellknown.Router},
	)

	if httpOpts.connectionManager == nil {
		httpOpts.connectionManager = &http_conn.HttpConnectionManager{}
	}

	connectionManager := httpOpts.connectionManager
	connectionManager.CodecType = http_conn.HttpConnectionManager_AUTO
	connectionManager.AccessLog = []*accesslog.AccessLog{}
	connectionManager.HttpFilters = filters
	connectionManager.StatPrefix = httpOpts.statPrefix
	connectionManager.NormalizePath = proto.BoolTrue
	if httpOpts.useRemoteAddress {
		connectionManager.UseRemoteAddress = proto.BoolTrue
	} else {
		connectionManager.UseRemoteAddress = proto.BoolFalse
	}

	// Allow websocket upgrades
	websocketUpgrade := &http_conn.HttpConnectionManager_UpgradeConfig{UpgradeType: "websocket"}
	connectionManager.UpgradeConfigs = []*http_conn.HttpConnectionManager_UpgradeConfig{websocketUpgrade}

	idleTimeout, err := time.ParseDuration(pluginParams.Node.Metadata.IdleTimeout)
	if idleTimeout > 0 && err == nil {
		if util.IsIstioVersionGE14(pluginParams.Node) {
			connectionManager.CommonHttpProtocolOptions = &core.HttpProtocolOptions{
				IdleTimeout: ptypes.DurationProto(idleTimeout),
			}
		} else {
			connectionManager.IdleTimeout = ptypes.DurationProto(idleTimeout)
		}
	}

	notimeout := ptypes.DurationProto(0 * time.Second)
	connectionManager.StreamIdleTimeout = notimeout

	if httpOpts.rds != "" {
		rds := &http_conn.HttpConnectionManager_Rds{
			Rds: &http_conn.Rds{
				ConfigSource: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					InitialFetchTimeout: features.InitialFetchTimeout,
				},
				RouteConfigName: httpOpts.rds,
			},
		}
		connectionManager.RouteSpecifier = rds
	} else {
		connectionManager.RouteSpecifier = &http_conn.HttpConnectionManager_RouteConfig{RouteConfig: httpOpts.routeConfig}
	}

	if pluginParams.Push.Mesh.AccessLogFile != "" {
		fl := &accesslogconfig.FileAccessLog{
			Path: pluginParams.Push.Mesh.AccessLogFile,
		}
		buildAccessLog(pluginParams.Node, fl, pluginParams.Push)
		acc := &accesslog.AccessLog{
			Name:       wellknown.FileAccessLog,
			ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
		}
		connectionManager.AccessLog = append(connectionManager.AccessLog, acc)
	}

	if pluginParams.Push.Mesh.EnableEnvoyAccessLogService {
		fl := &accesslogconfig.HttpGrpcAccessLogConfig{
			CommonConfig: &accesslogconfig.CommonGrpcAccessLogConfig{
				LogName: httpEnvoyAccessLogFriendlyName,
				GrpcService: &core.GrpcService{
					TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
							ClusterName: EnvoyAccessLogCluster,
						},
					},
				},
			},
		}

		if util.IsIstioVersionGE14(pluginParams.Node) {
			fl.CommonConfig.FilterStateObjectsToLog = envoyWasmStateToLog
		}

		acc := &accesslog.AccessLog{
			Name:       wellknown.HTTPGRPCAccessLog,
			ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
		}

		connectionManager.AccessLog = append(connectionManager.AccessLog, acc)
	}

	if pluginParams.Push.Mesh.EnableTracing {
		tc := authn_model.GetTraceConfig()
		connectionManager.Tracing = &http_conn.HttpConnectionManager_Tracing{
			ClientSampling: &envoy_type.Percent{
				Value: tc.ClientSampling,
			},
			RandomSampling: &envoy_type.Percent{
				Value: tc.RandomSampling,
			},
			OverallSampling: &envoy_type.Percent{
				Value: tc.OverallSampling,
			},
		}
		connectionManager.GenerateRequestId = proto.BoolTrue
	}

	return connectionManager
}

func buildThriftRatelimit(domain string, thriftconfig *meshconfig.MeshConfig_ThriftConfig) *thrift_ratelimit.RateLimit {
	thriftRateLimit := &thrift_ratelimit.RateLimit{
		Domain:          domain,
		Timeout:         ptypes.DurationProto(ThriftRLSDefaultTimeoutMS * time.Millisecond),
		FailureModeDeny: false,
		RateLimitService: &ratelimit.RateLimitServiceConfig{
			GrpcService: &core.GrpcService{},
		},
	}

	rlsClusterName, err := thritRLSClusterNameFromAuthority(thriftconfig.RateLimitUrl)
	if err != nil {
		log.Errorf("unable to generate thrift rls cluster name: %s\n", rlsClusterName)
		return nil
	}

	thriftRateLimit.RateLimitService.GrpcService.TargetSpecifier = &core.GrpcService_EnvoyGrpc_{
		EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
			ClusterName: rlsClusterName,
		},
	}

	if meshConfigTimeout := thriftconfig.GetRateLimitTimeout(); meshConfigTimeout != nil {
		thriftRateLimit.Timeout = gogo.DurationToProtoDuration(meshConfigTimeout)
	}

	if err := thriftRateLimit.Validate(); err != nil {
		log.Warn(err.Error())
	}

	return thriftRateLimit
}

func buildThriftProxy(thriftOpts *thriftListenerOpts) *thrift_proxy.ThriftProxy {
	return &thrift_proxy.ThriftProxy{
		Transport:   thriftOpts.transport,
		Protocol:    thriftOpts.protocol,
		RouteConfig: thriftOpts.routeConfig,
	}
}

// buildListener builds and initializes a Listener proto based on the provided opts. It does not set any filters.
func buildListener(opts buildListenerOpts) *xdsapi.Listener {
	filterChains := make([]*listener.FilterChain, 0, len(opts.filterChainOpts))
	listenerFiltersMap := make(map[string]bool)
	var listenerFilters []*listener.ListenerFilter

	// add a TLS inspector if we need to detect ServerName or ALPN
	needTLSInspector := false
	for _, chain := range opts.filterChainOpts {
		needsALPN := chain.tlsContext != nil && chain.tlsContext.CommonTlsContext != nil && len(chain.tlsContext.CommonTlsContext.AlpnProtocols) > 0
		if len(chain.sniHosts) > 0 || needsALPN {
			needTLSInspector = true
			break
		}
	}
	if needTLSInspector || opts.needHTTPInspector {
		listenerFiltersMap[wellknown.TlsInspector] = true
		listenerFilters = append(listenerFilters, &listener.ListenerFilter{Name: wellknown.TlsInspector})
	}

	if opts.needHTTPInspector {
		listenerFiltersMap[wellknown.HttpInspector] = true
		listenerFilters = append(listenerFilters, &listener.ListenerFilter{Name: wellknown.HttpInspector})
	}

	for _, chain := range opts.filterChainOpts {
		for _, filter := range chain.listenerFilters {
			if _, exist := listenerFiltersMap[filter.Name]; !exist {
				listenerFiltersMap[filter.Name] = true
				listenerFilters = append(listenerFilters, filter)
			}
		}
		match := &listener.FilterChainMatch{}
		needMatch := false
		if chain.match != nil {
			needMatch = true
			match = chain.match
		}
		if len(chain.sniHosts) > 0 {
			sort.Strings(chain.sniHosts)
			fullWildcardFound := false
			for _, h := range chain.sniHosts {
				if h == "*" {
					fullWildcardFound = true
					// If we have a host with *, it effectively means match anything, i.e.
					// no SNI based matching for this host.
					break
				}
			}
			if !fullWildcardFound {
				match.ServerNames = chain.sniHosts
			}
		}
		if len(chain.destinationCIDRs) > 0 {
			sort.Strings(chain.destinationCIDRs)
			for _, d := range chain.destinationCIDRs {
				if len(d) == 0 {
					continue
				}
				cidr := util.ConvertAddressToCidr(d)
				if cidr != nil && cidr.AddressPrefix != constants.UnspecifiedIP {
					match.PrefixRanges = append(match.PrefixRanges, cidr)
				}
			}
		}

		if !needMatch && reflect.DeepEqual(*match, listener.FilterChainMatch{}) {
			match = nil
		}
		filterChains = append(filterChains, &listener.FilterChain{
			FilterChainMatch: match,
			TransportSocket:  buildDownstreamTLSTransportSocket(chain.tlsContext),
		})
	}

	var deprecatedV1 *xdsapi.Listener_DeprecatedV1
	if !opts.bindToPort {
		deprecatedV1 = &xdsapi.Listener_DeprecatedV1{
			BindToPort: proto.BoolFalse,
		}
	}

	listener := &xdsapi.Listener{
		// TODO: need to sanitize the opts.bind if its a UDS socket, as it could have colons, that envoy
		// doesn't like
		Name:            opts.bind + "_" + strconv.Itoa(opts.port),
		Address:         util.BuildAddress(opts.bind, uint32(opts.port)),
		ListenerFilters: listenerFilters,
		FilterChains:    filterChains,
		DeprecatedV1:    deprecatedV1,
	}

	if util.IsIstioVersionGE13(opts.proxy) && opts.proxy.Type != model.Router {
		listener.ListenerFiltersTimeout = gogo.DurationToProtoDuration(opts.push.Mesh.ProtocolDetectionTimeout)

		if listener.ListenerFiltersTimeout != nil {
			listener.ContinueOnListenerFiltersTimeout = true
		}
	}

	return listener
}

// appendListenerFallthroughRoute adds a filter that will match all traffic and direct to the
// PassthroughCluster. This should be appended as the final filter or it will mask the others.
// This allows external https traffic, even when port the port (usually 443) is in use by another service.
func appendListenerFallthroughRoute(l *xdsapi.Listener, opts *buildListenerOpts,
	node *model.Proxy, currentListenerEntry *outboundListenerEntry) {
	wildcardMatch := &listener.FilterChainMatch{}
	for _, fc := range l.FilterChains {
		if isMatchAllFilterChain(fc) {
			// We can only have one wildcard match. If the filter chain already has one, skip it
			// This happens in the case of HTTP, which will get a fallthrough route added later,
			// or TCP, which is not supported
			return
		}
	}

	if currentListenerEntry != nil {
		for _, fc := range currentListenerEntry.listener.FilterChains {
			if isMatchAllFilterChain(fc) {
				// We can only have one wildcard match. If the existing filter chain already has one, skip it
				// This can happen when there are multiple https services
				return
			}
		}
	}

	fallthroughNetworkFilters := buildFallthroughNetworkFilters(opts.push, node)

	opts.filterChainOpts = append(opts.filterChainOpts, &filterChainOpts{
		networkFilters: fallthroughNetworkFilters,
		isFallThrough:  true,
	})
	l.FilterChains = append(l.FilterChains, &listener.FilterChain{FilterChainMatch: wildcardMatch})
}

// buildCompleteFilterChain adds the provided TCP and HTTP filters to the provided Listener and serializes them.
//
// TODO: should we change this from []plugins.FilterChains to [][]listener.Filter, [][]*http_conn.HttpFilter?
// TODO: given how tightly tied listener.FilterChains, opts.filterChainOpts, and mutable.FilterChains are to eachother
// we should encapsulate them some way to ensure they remain consistent (mainly that in each an index refers to the same
// chain)
func buildCompleteFilterChain(pluginParams *plugin.InputParams, mutable *plugin.MutableObjects, opts buildListenerOpts) error {
	if len(opts.filterChainOpts) == 0 {
		return fmt.Errorf("must have more than 0 chains in listener: %#v", mutable.Listener)
	}

	httpConnectionManagers := make([]*http_conn.HttpConnectionManager, len(mutable.FilterChains))
	thriftProxies := make([]*thrift_proxy.ThriftProxy, len(mutable.FilterChains))
	for i := range mutable.FilterChains {
		chain := mutable.FilterChains[i]
		opt := opts.filterChainOpts[i]
		mutable.Listener.FilterChains[i].Metadata = opt.metadata

		if opt.thriftOpts != nil && features.EnableThriftFilter.Get() {
			var quotas []model.Config
			// Add the TCP filters first.. and then the Thrift filter
			mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, chain.TCP...)

			thriftProxies[i] = buildThriftProxy(opt.thriftOpts)

			if pluginParams.Service != nil {
				quotas = opts.push.QuotaSpecByDestination(&model.ServiceInstance{
					Service: pluginParams.Service,
				})
			}

			// If the RLS service was provided, add the RLS to the Thrift filter
			// chain. Rate limiting is only applied client-side.
			if rlsURI := opts.push.Mesh.ThriftConfig.RateLimitUrl; rlsURI != "" &&
				mutable.Listener.TrafficDirection == core.TrafficDirection_OUTBOUND &&
				pluginParams.Service != nil &&
				pluginParams.Service.Hostname != "" &&
				len(quotas) > 0 {
				rateLimitConfig := buildThriftRatelimit(fmt.Sprint(pluginParams.Service.Hostname), opts.push.Mesh.ThriftConfig)
				rateLimitFilter := &thrift_proxy.ThriftFilter{
					Name: "envoy.filters.thrift.rate_limit",
				}
				routerFilter := &thrift_proxy.ThriftFilter{
					Name:       "envoy.filters.thrift.router",
					ConfigType: &thrift_proxy.ThriftFilter_TypedConfig{TypedConfig: util.MessageToAny(rateLimitConfig)},
				}

				thriftProxies[i].ThriftFilters = append(thriftProxies[i].ThriftFilters, rateLimitFilter, routerFilter)

			}

			filter := &listener.Filter{
				Name:       wellknown.ThriftProxy,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(thriftProxies[i])},
			}

			mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, filter)
			log.Debugf("attached Thrift filter with %d thrift_filter options to listener %q filter chain %d",
				len(thriftProxies[i].ThriftFilters), mutable.Listener.Name, i)
		} else if opt.httpOpts == nil {
			// we are building a network filter chain (no http connection manager) for this filter chain
			// In HTTP, we need to have mixer, RBAC, etc. upfront so that they can enforce policies immediately
			// For network filters such as mysql, mongo, etc., we need the filter codec upfront. Data from this
			// codec is used by RBAC or mixer later.

			if len(opt.networkFilters) > 0 {
				if opt.isFallThrough {
					insertFallthroughMetadata(mutable.Listener.FilterChains[i])
				}
				// this is the terminating filter
				lastNetworkFilter := opt.networkFilters[len(opt.networkFilters)-1]

				for n := 0; n < len(opt.networkFilters)-1; n++ {
					mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, opt.networkFilters[n])
				}
				mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, chain.TCP...)
				mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, lastNetworkFilter)
			} else {
				mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, chain.TCP...)
			}
			log.Debugf("attached %d network filters to listener %q filter chain %d", len(chain.TCP)+len(opt.networkFilters), mutable.Listener.Name, i)
		} else {
			// Add the TCP filters first.. and then the HTTP connection manager
			mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, chain.TCP...)

			opt.httpOpts.statPrefix = strings.ToLower(mutable.Listener.TrafficDirection.String()) + "_" + mutable.Listener.Name
			httpConnectionManagers[i] = buildHTTPConnectionManager(pluginParams, opt.httpOpts, chain.HTTP)
			filter := &listener.Filter{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(httpConnectionManagers[i])},
			}
			mutable.Listener.FilterChains[i].Filters = append(mutable.Listener.FilterChains[i].Filters, filter)
			log.Debugf("attached HTTP filter with %d http_filter options to listener %q filter chain %d",
				len(httpConnectionManagers[i].HttpFilters), mutable.Listener.Name, i)
		}
	}

	if !opts.skipUserFilters {
		// NOTE: we have constructed the HTTP connection manager filter above and we are passing the whole filter chain
		// EnvoyFilter crd could choose to replace the HTTP ConnectionManager that we built or can choose to add
		// more filters to the HTTP filter chain. In the latter case, the deprecatedInsertUserFilters function will
		// overwrite the HTTP connection manager in the filter chain after inserting the new filters
		return envoyfilter.DeprecatedInsertUserFilters(pluginParams, mutable.Listener, httpConnectionManagers)
	}

	return nil
}

// getActualWildcardAndLocalHost will return corresponding Wildcard and LocalHost
// depending on value of proxy's IPAddresses. This function checks each element
// and if there is at least one ipv4 address other than 127.0.0.1, it will use ipv4 address,
// if all addresses are ipv6  addresses then ipv6 address will be used to get wildcard and local host address.
func getActualWildcardAndLocalHost(node *model.Proxy) (string, string) {
	for i := 0; i < len(node.IPAddresses); i++ {
		addr := net.ParseIP(node.IPAddresses[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			return WildcardAddress, LocalhostAddress
		}
	}
	return WildcardIPv6Address, LocalhostIPv6Address
}

func ipv4AndIpv6Support(node *model.Proxy) (bool, bool) {
	ipv4, ipv6 := false, false
	for i := 0; i < len(node.IPAddresses); i++ {
		addr := net.ParseIP(node.IPAddresses[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	return ipv4, ipv6
}

// getSidecarInboundBindIP returns the IP that the proxy can bind to along with the sidecar specified port.
// It looks for an unicast address, if none found, then the default wildcard address is used.
// This will make the inbound listener bind to instance_ip:port instead of 0.0.0.0:port where applicable.
func getSidecarInboundBindIP(node *model.Proxy) string {
	defaultInboundIP, _ := getActualWildcardAndLocalHost(node)
	for _, ipAddr := range node.IPAddresses {
		ip := net.ParseIP(ipAddr)
		// Return the IP if its a global unicast address.
		if ip != nil && ip.IsGlobalUnicast() {
			return ip.String()
		}
	}
	return defaultInboundIP
}

func mergeTCPFilterChains(incoming []*listener.FilterChain, pluginParams *plugin.InputParams, listenerMapKey string,
	listenerMap map[string]*outboundListenerEntry, node *model.Proxy) []*listener.FilterChain {
	// TODO(rshriram) merge multiple identical filter chains with just a single destination CIDR based
	// filter chain match, into a single filter chain and array of destinationcidr matches

	// We checked TCP over HTTP, and HTTP over TCP conflicts above.
	// The code below checks for TCP over TCP conflicts and merges listeners

	// merge the newly built listener with the existing listener
	// if and only if the filter chains have distinct conditions
	// Extract the current filter chain matches
	// For every new filter chain match being added, check if any previous match is same
	// if so, skip adding this filter chain with a warning
	// This is very unoptimized.

	currentListenerEntry := listenerMap[listenerMapKey]
	newFilterChains := make([]*listener.FilterChain, 0,
		len(currentListenerEntry.listener.FilterChains)+len(incoming))
	newFilterChains = append(newFilterChains, currentListenerEntry.listener.FilterChains...)

	for _, incomingFilterChain := range incoming {
		conflictFound := false
		replacedFallthrough := false

	compareWithExisting:
		for i, existingFilterChain := range newFilterChains {
			if isMatchAllFilterChain(existingFilterChain) {
				// This is a catch all filter chain.
				// We can only merge with a non-catch all filter chain
				// Else mark it as conflict
				if isMatchAllFilterChain(incomingFilterChain) {
					// replace fallthrough filter chain with the real service one
					if isFallthroughFilterChain(existingFilterChain) {
						replacedFallthrough = true
						newFilterChains[i] = incomingFilterChain
						continue
					}
					// NOTE: While pluginParams.Service can be nil,
					// this code cannot be reached if Service is nil because a pluginParams.Service can be nil only
					// for user defined Egress listeners with ports. And these should occur in the API before
					// the wildcard egress listener. the check for the "locked" bit will eliminate the collision.
					// User is also not allowed to add duplicate ports in the egress listener
					var newHostname host.Name
					if pluginParams.Service != nil {
						newHostname = pluginParams.Service.Hostname
					} else {
						// user defined outbound listener via sidecar API
						newHostname = "sidecar-config-egress-tcp-listener"
					}

					conflictFound = true
					outboundListenerConflict{
						metric:          model.ProxyStatusConflictOutboundListenerTCPOverTCP,
						node:            pluginParams.Node,
						listenerName:    listenerMapKey,
						currentServices: currentListenerEntry.services,
						currentProtocol: currentListenerEntry.servicePort.Protocol,
						newHostname:     newHostname,
						newProtocol:     pluginParams.Port.Protocol,
					}.addMetric(node, pluginParams.Push)
					break compareWithExisting
				}
				continue
			}

			// We have two non-catch all filter chains. Check for duplicates
			if reflect.DeepEqual(existingFilterChain.FilterChainMatch, incomingFilterChain.FilterChainMatch) {
				var newHostname host.Name
				if pluginParams.Service != nil {
					newHostname = pluginParams.Service.Hostname
				} else {
					// user defined outbound listener via sidecar API
					newHostname = "sidecar-config-egress-tcp-listener"
				}

				conflictFound = true
				outboundListenerConflict{
					metric:          model.ProxyStatusConflictOutboundListenerTCPOverTCP,
					node:            pluginParams.Node,
					listenerName:    listenerMapKey,
					currentServices: currentListenerEntry.services,
					currentProtocol: currentListenerEntry.servicePort.Protocol,
					newHostname:     newHostname,
					newProtocol:     pluginParams.Port.Protocol,
				}.addMetric(node, pluginParams.Push)
				break compareWithExisting
			}
		}

		if !conflictFound && !replacedFallthrough {
			// There is no conflict with any filter chain in the existing listener.
			// So append the new filter chains to the existing listener's filter chains
			newFilterChains = append(newFilterChains, incomingFilterChain)
			if pluginParams.Service != nil {
				lEntry := listenerMap[listenerMapKey]
				lEntry.services = append(lEntry.services, pluginParams.Service)
			}
		}
	}

	return newFilterChains
}

func mergeFilterChains(httpFilterChain, tcpFilterChain []*listener.FilterChain) []*listener.FilterChain {
	var newFilterChan []*listener.FilterChain
	for _, fc := range httpFilterChain {
		if fc.FilterChainMatch == nil {
			fc.FilterChainMatch = &listener.FilterChainMatch{}
		}

		fc.FilterChainMatch.ApplicationProtocols = append(fc.FilterChainMatch.ApplicationProtocols, plaintextHTTPALPNs...)
		newFilterChan = append(newFilterChan, fc)

	}
	return append(tcpFilterChain, newFilterChan...)
}

func getPluginFilterChain(opts buildListenerOpts) []plugin.FilterChain {
	filterChain := make([]plugin.FilterChain, len(opts.filterChainOpts))

	for id := range filterChain {
		if opts.filterChainOpts[id].httpOpts == nil {
			filterChain[id].ListenerProtocol = plugin.ListenerProtocolTCP
		} else {
			filterChain[id].ListenerProtocol = plugin.ListenerProtocolHTTP
		}
		filterChain[id].IsFallThrough = opts.filterChainOpts[id].isFallThrough
	}

	return filterChain
}

// isConflictWithWellKnownPort checks conflicts between incoming protocol and existing protocol.
// Mongo and MySQL are not allowed to co-exist with other protocols in one port.
func isConflictWithWellKnownPort(incoming, existing protocol.Instance, conflict int) bool {
	if conflict == NoConflict {
		return true
	}

	if (incoming == protocol.Mongo ||
		incoming == protocol.MySQL ||
		existing == protocol.Mongo ||
		existing == protocol.MySQL) && incoming != existing {
		return false
	}

	return true
}

func appendListenerFilters(filters []*listener.ListenerFilter) []*listener.ListenerFilter {
	hasTLSInspector := false
	hasHTTPInspector := false

	for _, f := range filters {
		hasTLSInspector = hasTLSInspector || f.Name == wellknown.TlsInspector
		hasHTTPInspector = hasHTTPInspector || f.Name == wellknown.HttpInspector
	}

	if !hasTLSInspector {
		filters =
			append(filters, &listener.ListenerFilter{Name: wellknown.TlsInspector})
	}

	if !hasHTTPInspector {
		filters =
			append(filters, &listener.ListenerFilter{Name: wellknown.HttpInspector})
	}

	return filters
}

// nolint: interfacer
func buildDownstreamTLSTransportSocket(tlsContext *auth.DownstreamTlsContext) *core.TransportSocket {
	if tlsContext == nil {
		return nil
	}
	return &core.TransportSocket{Name: util.EnvoyTLSSocketName, ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsContext)}}
}

func insertFallthroughMetadata(chain *listener.FilterChain) {
	if chain.Metadata == nil {
		chain.Metadata = &core.Metadata{
			FilterMetadata: map[string]*structpb.Struct{},
		}
	}
	if chain.Metadata.FilterMetadata[PilotMetaKey] == nil {
		chain.Metadata.FilterMetadata[PilotMetaKey] = &structpb.Struct{
			Fields: map[string]*structpb.Value{},
		}
	}
	chain.Metadata.FilterMetadata[PilotMetaKey].Fields["fallthrough"] =
		&structpb.Value{Kind: &structpb.Value_BoolValue{BoolValue: true}}
}

func isMatchAllFilterChain(fc *listener.FilterChain) bool {
	if fc.FilterChainMatch == nil || reflect.DeepEqual(fc.FilterChainMatch, &listener.FilterChainMatch{}) {
		return true
	}
	return false
}

func isFallthroughFilterChain(fc *listener.FilterChain) bool {
	if fc.Metadata != nil && fc.Metadata.FilterMetadata != nil &&
		fc.Metadata.FilterMetadata[PilotMetaKey].Fields["fallthrough"].GetBoolValue() {
		return true
	}
	return false
}

func removeListenerFilterTimeout(listeners []*xdsapi.Listener) {
	for _, l := range listeners {
		// Remove listener filter timeout for
		// 	1. outbound listeners AND
		// 	2. without HTTP inspector
		hasHTTPInspector := false
		for _, lf := range l.ListenerFilters {
			if lf.Name == wellknown.HttpInspector {
				hasHTTPInspector = true
				break
			}
		}

		if !hasHTTPInspector && l.TrafficDirection == core.TrafficDirection_OUTBOUND {
			l.ListenerFiltersTimeout = nil
			l.ContinueOnListenerFiltersTimeout = false
		}
	}
}
