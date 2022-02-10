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

package common // import "istio.io/istio/pkg/test/framework/components/echo/common"

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var EchoPorts = []echo.Port{
	{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, InstancePort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, InstancePort: 17070},
	{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, InstancePort: 19090},
	{Name: "https", Protocol: protocol.HTTPS, ServicePort: 443, InstancePort: 18443, TLS: true},
	{Name: "tcp-server", Protocol: protocol.TCP, ServicePort: 9091, InstancePort: 16060, ServerFirst: true},
	{Name: "auto-tcp", Protocol: protocol.TCP, ServicePort: 9092, InstancePort: 19091},
	{Name: "auto-tcp-server", Protocol: protocol.TCP, ServicePort: 9093, InstancePort: 16061, ServerFirst: true},
	{Name: "auto-http", Protocol: protocol.HTTP, ServicePort: 81, InstancePort: 18081},
	{Name: "auto-grpc", Protocol: protocol.GRPC, ServicePort: 7071, InstancePort: 17071},
	{Name: "auto-https", Protocol: protocol.HTTPS, ServicePort: 9443, InstancePort: 19443, TLS: true},
	{Name: "http-instance", Protocol: protocol.HTTP, ServicePort: 82, InstancePort: 18082, InstanceIP: true},
	{Name: "http-localhost", Protocol: protocol.HTTP, ServicePort: 84, InstancePort: 18084, LocalhostIP: true},
}

var WorkloadPorts = []echo.WorkloadPort{
	{Protocol: protocol.TCP, Port: 19092},
	{Protocol: protocol.HTTP, Port: 18083},
}
