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

package ports

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// Port names.
var (
	HTTP             = echo.Port{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, WorkloadPort: 18080}
	GRPC             = echo.Port{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, WorkloadPort: 17070}
	HTTP2            = echo.Port{Name: "http2", Protocol: protocol.HTTP, ServicePort: 85, WorkloadPort: 18085}
	TCP              = echo.Port{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, WorkloadPort: 19090}
	HTTPS            = echo.Port{Name: "https", Protocol: protocol.HTTPS, ServicePort: 443, WorkloadPort: 18443, TLS: true}
	TCPServer        = echo.Port{Name: "tcp-server", Protocol: protocol.TCP, ServicePort: 9091, WorkloadPort: 16060, ServerFirst: true}
	AutoTCP          = echo.Port{Name: "auto-tcp", Protocol: protocol.TCP, ServicePort: 9092, WorkloadPort: 19091}
	AutoTCPServer    = echo.Port{Name: "auto-tcp-server", Protocol: protocol.TCP, ServicePort: 9093, WorkloadPort: 16061, ServerFirst: true}
	AutoHTTP         = echo.Port{Name: "auto-http", Protocol: protocol.HTTP, ServicePort: 81, WorkloadPort: 18081}
	AutoGRPC         = echo.Port{Name: "auto-grpc", Protocol: protocol.GRPC, ServicePort: 7071, WorkloadPort: 17071}
	AutoHTTPS        = echo.Port{Name: "auto-https", Protocol: protocol.HTTPS, ServicePort: 9443, WorkloadPort: 19443, TLS: true}
	HTTPInstance     = echo.Port{Name: "http-instance", Protocol: protocol.HTTP, ServicePort: 82, WorkloadPort: 18082, InstanceIP: true}
	HTTPLocalHost    = echo.Port{Name: "http-localhost", Protocol: protocol.HTTP, ServicePort: 84, WorkloadPort: 18084, LocalhostIP: true}
	TCPWorkloadOnly  = echo.Port{Name: "tcp-wl-only", Protocol: protocol.TCP, ServicePort: echo.NoServicePort, WorkloadPort: 19092}
	HTTPWorkloadOnly = echo.Port{Name: "http-wl-only", Protocol: protocol.HTTP, ServicePort: echo.NoServicePort, WorkloadPort: 18083}
	TCPForHTTP       = echo.Port{Name: "tcp-for-http", Protocol: protocol.HTTP, ServicePort: 86, WorkloadPort: 18086}
	HTTPWithProxy    = echo.Port{Name: "tcp-proxy", Protocol: protocol.HTTP, ServicePort: 87, WorkloadPort: 18087, ProxyProtocol: true}
	// MTLS is a special port, which induces requirements most "in-mesh" pods don't meet presently. It will not be added to our default "All" list as a result.
	MTLS = echo.Port{Name: "mtls", Protocol: protocol.HTTPS, ServicePort: 4443, WorkloadPort: 18444, MTLS: true}
)

// All the common ports.
func All() echo.Ports {
	return echo.Ports{
		HTTP,
		GRPC,
		HTTP2,
		TCP,
		HTTPS,
		TCPServer,
		AutoTCP,
		AutoTCPServer,
		AutoHTTP,
		AutoGRPC,
		AutoHTTPS,
		HTTPInstance,
		HTTPLocalHost,
		TCPWorkloadOnly,
		HTTPWorkloadOnly,
		TCPForHTTP,
		HTTPWithProxy,
	}
}

// Headless returns a modified version of All for use with headless services.
func Headless() echo.Ports {
	all := All()
	headlessPorts := make([]echo.Port, len(all))
	for i, p := range all {
		if !p.IsWorkloadOnly() {
			p.ServicePort = p.WorkloadPort
		}
		headlessPorts[i] = p
	}
	return headlessPorts
}
