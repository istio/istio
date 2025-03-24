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

// Package wellknown contains common names for filters, listeners, etc.
// copied from envoyproxy/go-control-plane.
// TODO: remove this package
package wellknown

// HTTP filter names
const (
	// Buffer HTTP filter
	Buffer = "envoy.filters.http.buffer"
	// CORS HTTP filter
	CORS = "envoy.filters.http.cors"
	// Dynamo HTTP filter
	Dynamo = "envoy.filters.http.dynamo"
	// Fault HTTP filter
	Fault = "envoy.filters.http.fault"
	// GRPCHTTP1Bridge HTTP filter
	GRPCHTTP1Bridge = "envoy.filters.http.grpc_http1_bridge"
	// GRPCJSONTranscoder HTTP filter
	GRPCJSONTranscoder = "envoy.filters.http.grpc_json_transcoder"
	// GRPCWeb HTTP filter
	GRPCWeb = "envoy.filters.http.grpc_web"
	// Gzip HTTP filter
	Gzip = "envoy.filters.http.gzip"
	// IPTagging HTTP filter
	IPTagging = "envoy.filters.http.ip_tagging"
	// HTTPRateLimit filter
	HTTPRateLimit = "envoy.filters.http.ratelimit"
	// Router HTTP filter
	Router = "envoy.filters.http.router"
	// Health checking HTTP filter
	HealthCheck = "envoy.filters.http.health_check"
	// Lua HTTP filter
	Lua = "envoy.filters.http.lua"
	// Squash HTTP filter
	Squash = "envoy.filters.http.squash"
	// HTTPExternalAuthorization HTTP filter
	HTTPExternalAuthorization = "envoy.filters.http.ext_authz"
	// HTTPRoleBasedAccessControl HTTP filter
	HTTPRoleBasedAccessControl = "envoy.filters.http.rbac"
	// HTTPGRPCStats HTTP filter
	HTTPGRPCStats = "envoy.filters.http.grpc_stats"
	// HTTP WASM filter
	HTTPWasm = "envoy.extensions.filters.http.wasm.v3.Wasm"
	// HTTPExternalProcessing HTTP filter
	HTTPExternalProcessing = "envoy.filters.http.ext_proc"
)

// Network filter names
const (
	// ClientSSLAuth network filter
	ClientSSLAuth = "envoy.filters.network.client_ssl_auth"
	// Echo network filter
	Echo = "envoy.filters.network.echo"
	// HTTPConnectionManager network filter
	HTTPConnectionManager = "envoy.filters.network.http_connection_manager"
	// TCPProxy network filter
	TCPProxy = "envoy.filters.network.tcp_proxy"
	// RateLimit network filter
	RateLimit = "envoy.filters.network.ratelimit"
	// MongoProxy network filter
	MongoProxy = "envoy.filters.network.mongo_proxy"
	// ThriftProxy network filter
	ThriftProxy = "envoy.filters.network.thrift_proxy"
	// RedisProxy network filter
	RedisProxy = "envoy.filters.network.redis_proxy"
	// MySQLProxy network filter
	MySQLProxy = "envoy.filters.network.mysql_proxy"
	// ExternalAuthorization network filter
	ExternalAuthorization = "envoy.filters.network.ext_authz"
	// RoleBasedAccessControl network filter
	RoleBasedAccessControl = "envoy.filters.network.rbac"
)

// Listener filter names
const (
	// OriginalDestination listener filter
	OriginalDestination = "envoy.filters.listener.original_dst"
	// ProxyProtocol listener filter
	ProxyProtocol = "envoy.filters.listener.proxy_protocol"
	// TLSInspector listener filter
	TLSInspector = "envoy.filters.listener.tls_inspector" // nolint:golint,revive
	// HTTPInspector listener filter
	HTTPInspector = "envoy.filters.listener.http_inspector"
	// OriginalSource listener filter
	OriginalSource = "envoy.filters.listener.original_src"
)

// Access log sink names
const (
	// FileAccessLog sink name
	FileAccessLog = "envoy.access_loggers.file"
	// HTTPGRPCAccessLog sink for the HTTP gRPC access log service
	HTTPGRPCAccessLog = "envoy.access_loggers.http_grpc"
)

// Transport socket names
const (
	// TransportSocket RawBuffer
	TransportSocketRawBuffer = "envoy.transport_sockets.raw_buffer"
	// TransportSocketTLS labels the "envoy.transport_sockets.tls" filter.
	TransportSocketTLS = "envoy.transport_sockets.tls"
	// TransportSocket Quic
	TransportSocketQuic = "envoy.transport_sockets.quic"
	// TransportSocketPROXY indicates upstream HA-PROXY protocol
	TransportSocketPROXY = "envoy.transport_sockets.upstream_proxy_protocol"
)
