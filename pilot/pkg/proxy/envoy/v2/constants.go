// Copyright 2018 Istio Authors
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


package v2

import "time"

const (
	// DefaultAccessLog is the name of the log channel (stdout in docker environment)
	DefaultAccessLog = "/dev/stdout"

	// DefaultLbType defines the default load balancer policy
	DefaultLbType = LbTypeRoundRobin

	// LDSName is the name of listener-discovery-service (LDS) cluster
	LDSName = "lds"

	// RDSName is the name of route-discovery-service (RDS) cluster
	RDSName = "rds"

	// SDSName is the name of service-discovery-service (SDS) cluster
	SDSName = "sds"

	// CDSName is the name of cluster-discovery-service (CDS) cluster
	CDSName = "cds"

	// RDSAll is the special name for HTTP PROXY route
	RDSAll = "http_proxy"

	// VirtualListenerName is the name for traffic capture listener
	VirtualListenerName = "virtual"

	// ClusterTypeStrictDNS name for clusters of type 'strict_dns'
	ClusterTypeStrictDNS = "strict_dns"

	// ClusterTypeStatic name for clusters of type 'static'
	ClusterTypeStatic = "static"

	// ClusterTypeOriginalDST name for clusters of type 'original_dst'
	ClusterTypeOriginalDST = "original_dst"

	// ClusterTypeSDS name for clusters of type 'sds'
	ClusterTypeSDS = "sds"

	// LbTypeRoundRobin is the name for round-robin LB
	LbTypeRoundRobin = "round_robin"

	// LbTypeLeastRequest is the name for least request LB
	LbTypeLeastRequest = "least_request"

	// LbTypeRingHash is the name for ring hash LB
	LbTypeRingHash = "ring_hash"

	// LbTypeRandom is the name for random LB
	LbTypeRandom = "random"

	// LbTypeOriginalDST is the name for LB of original_dst
	LbTypeOriginalDST = "original_dst_lb"

	// ClusterFeatureHTTP2 is the feature to use HTTP/2 for a cluster
	ClusterFeatureHTTP2 = "http2"

	// HTTPConnectionManager is the name of HTTP filter.
	HTTPConnectionManager = "http_connection_manager"

	// TCPProxyFilter is the name of the TCP Proxy network filter.
	TCPProxyFilter = "tcp_proxy"

	// CORSFilter is the name of the CORS network filter
	CORSFilter = "cors"

	// MongoProxyFilter is the name of the Mongo Proxy network filter.
	MongoProxyFilter = "mongo_proxy"

	// RedisProxyFilter is the name of the Redis Proxy network filter.
	RedisProxyFilter = "redis_proxy"

	// RedisDefaultOpTimeout is the op timeout used for Redis Proxy filter
	// Currently it is set to 30s (conversion happens in the filter)
	// TODO - Allow this to be configured.
	RedisDefaultOpTimeout = 30 * time.Second

	// WildcardAddress binds to all IP addresses
	WildcardAddress = "0.0.0.0"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"

	// EgressTraceOperation denotes the name of trace operation for Envoy
	EgressTraceOperation = "egress"

	// IngressTraceOperation denotes the name of trace operation for Envoy
	IngressTraceOperation = "ingress"

	// ZipkinTraceDriverType denotes the Zipkin HTTP trace driver
	ZipkinTraceDriverType = "zipkin"

	// ZipkinCollectorCluster denotes the cluster where zipkin server is running
	ZipkinCollectorCluster = "zipkin"

	// ZipkinCollectorEndpoint denotes the REST endpoint where Envoy posts Zipkin spans
	ZipkinCollectorEndpoint = "/api/v1/spans"

	// MaxClusterNameLength is the maximum cluster name length
	MaxClusterNameLength = 189 // TODO: use MeshConfig.StatNameLength instead

	// MixerFilter name and its attributes
	MixerFilter = "mixer"

	// Headers with special meaning in Envoy

	// HeaderMethod is the method header.
	HeaderMethod = ":method"
	// HeaderAuthority is the authority header.
	HeaderAuthority = ":authority"
	// HeaderScheme is the scheme header.
	HeaderScheme = ":scheme"

	router  = "router"
	auto    = "auto"
	decoder = "decoder"
	read    = "read"
	both    = "both"
)
