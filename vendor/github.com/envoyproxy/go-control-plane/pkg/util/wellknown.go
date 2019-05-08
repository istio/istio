// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package util

// HTTP filter names
const (
	// Buffer HTTP filter
	Buffer = "envoy.buffer"
	// CORS HTTP filter
	CORS = "envoy.cors"
	// Dynamo HTTP filter
	Dynamo = "envoy.http_dynamo_filter"
	// Fault HTTP filter
	Fault = "envoy.fault"
	// GRPCHTTP1Bridge HTTP filter
	GRPCHTTP1Bridge = "envoy.grpc_http1_bridge"
	// GRPCJSONTranscoder HTTP filter
	GRPCJSONTranscoder = "envoy.grpc_json_transcoder"
	// GRPCWeb HTTP filter
	GRPCWeb = "envoy.grpc_web"
	// Gzip HTTP filter
	Gzip = "envoy.gzip"
	// IPTagging HTTP filter
	IPTagging = "envoy.ip_tagging"
	// HTTPRateLimit filter
	HTTPRateLimit = "envoy.rate_limit"
	// Router HTTP filter
	Router = "envoy.router"
	// Health checking HTTP filter
	HealthCheck = "envoy.health_check"
	// Lua HTTP filter
	Lua = "envoy.lua"
	// Squash HTTP filter
	Squash = "envoy.squash"
	// HTTPExternalAuthorization HTTP filter
	HTTPExternalAuthorization = "envoy.ext_authz"
)

// Network filter names
const (
	// ClientSSLAuth network filter
	ClientSSLAuth = "envoy.client_ssl_auth"
	// Echo network filter
	Echo = "envoy.echo"
	// HTTPConnectionManager network filter
	HTTPConnectionManager = "envoy.http_connection_manager"
	// TCPProxy network filter
	TCPProxy = "envoy.tcp_proxy"
	// RateLimit network filter
	RateLimit = "envoy.ratelimit"
	// MongoProxy network filter
	MongoProxy = "envoy.mongo_proxy"
	// ThriftProxy network filter
	ThriftProxy = "envoy.filters.network.thrift_proxy"
	// RedisProxy network filter
	RedisProxy = "envoy.redis_proxy"
	// MySQLProxy network filter
	MySQLProxy = "envoy.filters.network.mysql_proxy"
	// ExternalAuthorization network filter
	ExternalAuthorization = "envoy.ext_authz"
)

// Listener filter names
const (
	// OriginalDestination listener filter
	OriginalDestination = "envoy.listener.original_dst"
	// ProxyProtocol listener filter
	ProxyProtocol = "envoy.listener.proxy_protocol"
	// TlsInspector listener filter
	TlsInspector = "envoy.listener.tls_inspector"
)

// Tracing provider names
const (
	// Lightstep tracer name
	Lightstep = "envoy.lightstep"
	// Zipkin tracer name
	Zipkin = "envoy.zipkin"
	// DynamicOT tracer name
	DynamicOT = "envoy.dynamic.ot"
)

// Stats sink names
const (
	// Statsd sink
	Statsd = "envoy.statsd"
	// DogStatsD compatible stastsd sink
	DogStatsd = "envoy.dog_statsd"
	// MetricsService sink
	MetricsService = "envoy.metrics_service"
)

// Access log sink names
const (
	// FileAccessLog sink name
	FileAccessLog = "envoy.file_access_log"
	// HTTPGRPCAccessLog sink for the HTTP gRPC access log service
	HTTPGRPCAccessLog = "envoy.http_grpc_access_log"
)
