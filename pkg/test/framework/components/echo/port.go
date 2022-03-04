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

package echo

import (
	"fmt"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
)

// Port exposed by an Echo Instance
type Port struct {
	// Name of this port
	Name string

	// Protocol to be used for the port.
	Protocol protocol.Instance

	// ServicePort number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service.
	ServicePort int

	// InstancePort number where this instance is listening for connections.
	// This need not be the same as the ServicePort where the service is accessed.
	InstancePort int

	// TLS determines whether the connection will be plain text or TLS. By default this is false (plain text).
	TLS bool

	// ServerFirst determines whether the port will use server first communication, meaning the client will not send the first byte.
	ServerFirst bool

	// InstanceIP determines if echo will listen on the instance IP; otherwise, it will listen on wildcard
	InstanceIP bool

	// LocalhostIP determines if echo will listen on the localhost IP; otherwise, it will listen on wildcard
	LocalhostIP bool
}

// Scheme infers the scheme to be used based on the Protocol.
func (p Port) Scheme() (scheme.Instance, error) {
	switch p.Protocol {
	case protocol.GRPC, protocol.GRPCWeb, protocol.HTTP2:
		return scheme.GRPC, nil
	case protocol.HTTP:
		return scheme.HTTP, nil
	case protocol.HTTPS:
		return scheme.HTTPS, nil
	case protocol.TCP:
		return scheme.TCP, nil
	default:
		return "", fmt.Errorf("failed creating call for port %s: unsupported protocol %s",
			p.Name, p.Protocol)
	}
}
