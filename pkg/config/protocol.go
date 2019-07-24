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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package config

import "strings"

// Protocol defines network protocols for ports
type Protocol string

const (
	// ProtocolGRPC declares that the port carries gRPC traffic.
	ProtocolGRPC Protocol = "GRPC"
	// ProtocolGRPCWeb declares that the port carries gRPC traffic.
	ProtocolGRPCWeb Protocol = "GRPC-Web"
	// ProtocolHTTP declares that the port carries HTTP/1.1 traffic.
	// Note that HTTP/1.0 or earlier may not be supported by the proxy.
	ProtocolHTTP Protocol = "HTTP"
	// ProtocolHTTP2 declares that the port carries HTTP/2 traffic.
	ProtocolHTTP2 Protocol = "HTTP2"
	// ProtocolHTTPS declares that the port carries HTTPS traffic.
	ProtocolHTTPS Protocol = "HTTPS"
	// ProtocolTCP declares the the port uses TCP.
	// This is the default protocol for a service port.
	ProtocolTCP Protocol = "TCP"
	// ProtocolTLS declares that the port carries TLS traffic.
	// TLS traffic is assumed to contain SNI as part of the handshake.
	ProtocolTLS Protocol = "TLS"
	// ProtocolTLS declares that the port carries Thrift traffic.
	ProtocolThrift Protocol = "Thrift"
	// ProtocolUDP declares that the port uses UDP.
	// Note that UDP protocol is not currently supported by the proxy.
	ProtocolUDP Protocol = "UDP"
	// ProtocolMongo declares that the port carries MongoDB traffic.
	ProtocolMongo Protocol = "Mongo"
	// ProtocolRedis declares that the port carries Redis traffic.
	ProtocolRedis Protocol = "Redis"
	// ProtocolMySQL declares that the port carries MySQL traffic.
	ProtocolMySQL Protocol = "MySQL"
	// ProtocolUnsupported - value to signify that the protocol is unsupported.
	ProtocolUnsupported Protocol = "UnsupportedProtocol"
)

// ParseProtocol from string ignoring case
func ParseProtocol(s string) Protocol {
	switch strings.ToLower(s) {
	case "tcp":
		return ProtocolTCP
	case "udp":
		return ProtocolUDP
	case "grpc":
		return ProtocolGRPC
	case "grpc-web":
		return ProtocolGRPCWeb
	case "http":
		return ProtocolHTTP
	case "http2":
		return ProtocolHTTP2
	case "https":
		return ProtocolHTTPS
	case "tls":
		return ProtocolTLS
	case "mongo":
		return ProtocolMongo
	case "redis":
		return ProtocolRedis
	case "mysql":
		return ProtocolMySQL
	}

	return ProtocolUnsupported
}

// IsHTTP2 is true for protocols that use HTTP/2 as transport protocol
func (p Protocol) IsHTTP2() bool {
	switch p {
	case ProtocolHTTP2, ProtocolGRPC, ProtocolGRPCWeb:
		return true
	default:
		return false
	}
}

// IsHTTP is true for protocols that use HTTP as transport protocol
func (p Protocol) IsHTTP() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTP2, ProtocolGRPC, ProtocolGRPCWeb:
		return true
	default:
		return false
	}
}

// IsTCP is true for protocols that use TCP as transport protocol
func (p Protocol) IsTCP() bool {
	switch p {
	case ProtocolTCP, ProtocolHTTPS, ProtocolTLS, ProtocolMongo, ProtocolRedis, ProtocolMySQL:
		return true
	default:
		return false
	}
}

// IsTLS is true for protocols on top of TLS (e.g. HTTPS)
func (p Protocol) IsTLS() bool {
	switch p {
	case ProtocolHTTPS, ProtocolTLS:
		return true
	default:
		return false
	}
}
