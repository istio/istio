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
const (
	HTTP             = "http"
	GRPC             = "grpc"
	HTTP2            = "http2"
	TCP              = "tcp"
	HTTPS            = "https"
	TCPServer        = "tcp-server"
	AutoTCP          = "auto-tcp"
	AutoTCPServer    = "auto-tcp-server"
	AutoHTTP         = "auto-http"
	AutoGRPC         = "auto-grpc"
	AutoHTTPS        = "auto-https"
	HTTPInstance     = "http-instance"
	HTTPLocalHost    = "http-localhost"
	TCPWorkloadOnly  = "tcp-wl-only"
	HTTPWorkloadOnly = "http-wl-only"
)

// All the common ports.
func All() echo.Ports {
	return echo.Ports{
		{Name: HTTP, Protocol: protocol.HTTP, ServicePort: 80, WorkloadPort: 18080},
		{Name: GRPC, Protocol: protocol.GRPC, ServicePort: 7070, WorkloadPort: 17070},
		{Name: HTTP2, Protocol: protocol.HTTP, ServicePort: 85, WorkloadPort: 18085},
		{Name: TCP, Protocol: protocol.TCP, ServicePort: 9090, WorkloadPort: 19090},
		{Name: HTTPS, Protocol: protocol.HTTPS, ServicePort: 443, WorkloadPort: 18443, TLS: true},
		{Name: TCPServer, Protocol: protocol.TCP, ServicePort: 9091, WorkloadPort: 16060, ServerFirst: true},
		{Name: AutoTCP, Protocol: protocol.TCP, ServicePort: 9092, WorkloadPort: 19091},
		{Name: AutoTCPServer, Protocol: protocol.TCP, ServicePort: 9093, WorkloadPort: 16061, ServerFirst: true},
		{Name: AutoHTTP, Protocol: protocol.HTTP, ServicePort: 81, WorkloadPort: 18081},
		{Name: AutoGRPC, Protocol: protocol.GRPC, ServicePort: 7071, WorkloadPort: 17071},
		{Name: AutoHTTPS, Protocol: protocol.HTTPS, ServicePort: 9443, WorkloadPort: 19443, TLS: true},
		{Name: HTTPInstance, Protocol: protocol.HTTP, ServicePort: 82, WorkloadPort: 18082, InstanceIP: true},
		{Name: HTTPLocalHost, Protocol: protocol.HTTP, ServicePort: 84, WorkloadPort: 18084, LocalhostIP: true},
		{Name: TCPWorkloadOnly, Protocol: protocol.TCP, ServicePort: echo.NoServicePort, WorkloadPort: 19092},
		{Name: HTTPWorkloadOnly, Protocol: protocol.HTTP, ServicePort: echo.NoServicePort, WorkloadPort: 18083},
	}
}

// Headless returns a modified version of All for use with headless services.
func Headless() echo.Ports {
	all := All()
	headlessPorts := make([]echo.Port, len(all))
	for i, p := range all {
		p.ServicePort = p.WorkloadPort
		headlessPorts[i] = p
	}
	return headlessPorts
}
