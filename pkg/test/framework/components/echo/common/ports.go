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

package common

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var Ports = echo.Ports{
	// Service ports.
	{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, WorkloadPort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, WorkloadPort: 17070},
	{Name: "http2", Protocol: protocol.HTTP, ServicePort: 85, WorkloadPort: 18085},
	{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, WorkloadPort: 19090},
	{Name: "https", Protocol: protocol.HTTPS, ServicePort: 443, WorkloadPort: 18443, TLS: true},
	{Name: "tcp-server", Protocol: protocol.TCP, ServicePort: 9091, WorkloadPort: 16060, ServerFirst: true},
	{Name: "auto-tcp", Protocol: protocol.TCP, ServicePort: 9092, WorkloadPort: 19091},
	{Name: "auto-tcp-server", Protocol: protocol.TCP, ServicePort: 9093, WorkloadPort: 16061, ServerFirst: true},
	{Name: "auto-http", Protocol: protocol.HTTP, ServicePort: 81, WorkloadPort: 18081},
	{Name: "auto-grpc", Protocol: protocol.GRPC, ServicePort: 7071, WorkloadPort: 17071},
	{Name: "auto-https", Protocol: protocol.HTTPS, ServicePort: 9443, WorkloadPort: 19443, TLS: true},
	{Name: "http-instance", Protocol: protocol.HTTP, ServicePort: 82, WorkloadPort: 18082, InstanceIP: true},
	{Name: "http-localhost", Protocol: protocol.HTTP, ServicePort: 84, WorkloadPort: 18084, LocalhostIP: true},

	// Workload-only ports.
	{Name: "tcp-wl-only", Protocol: protocol.TCP, ServicePort: echo.NoServicePort, WorkloadPort: 19092},
	{Name: "http-wl-only", Protocol: protocol.HTTP, ServicePort: echo.NoServicePort, WorkloadPort: 18083},
}
