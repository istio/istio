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

package kube

import (
	"strings"

	"istio.io/istio/pkg/config/protocol"

	coreV1 "k8s.io/api/core/v1"
)

const (
	SMTP    = 25
	DNS     = 53
	MySQL   = 3306
	MongoDB = 27017
)

var (
	// Ports be skipped for protocol sniffing. Applications bound to these ports will be broken if
	// protocol sniffing is enabled.
	wellKnownPorts = map[int32]struct{}{
		SMTP:    {},
		DNS:     {},
		MySQL:   {},
		MongoDB: {},
	}
)

var grpcWeb = string(protocol.GRPCWeb)
var grpcWebLen = len(grpcWeb)

// ConvertProtocol from k8s protocol and port name
func ConvertProtocol(port int32, portName string, proto coreV1.Protocol, appProto *string) protocol.Instance {
	if proto == coreV1.ProtocolUDP {
		return protocol.UDP
	}

	// If application protocol is set, we will use that
	// If not, use the port name
	name := portName
	if appProto != nil {
		name = *appProto
	}

	// Check if the port name prefix is "grpc-web". Need to do this before the general
	// prefix check below, since it contains a hyphen.
	if len(name) >= grpcWebLen && strings.EqualFold(name[:grpcWebLen], grpcWeb) {
		return protocol.GRPCWeb
	}

	// Parse the port name to find the prefix, if any.
	i := strings.IndexByte(name, '-')
	if i >= 0 {
		name = name[:i]
	}

	p := protocol.Parse(name)
	if p == protocol.Unsupported {
		// Make TCP as default protocol for well know ports if protocol is not specified.
		if _, has := wellKnownPorts[port]; has {
			return protocol.TCP
		}
	}
	return p
}
