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

package networking

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/config/protocol"
)

// ListenerProtocol is the protocol associated with the listener.
type ListenerProtocol int

const (
	// ListenerProtocolUnknown is an unknown type of listener.
	ListenerProtocolUnknown = iota
	// ListenerProtocolTCP is a TCP listener.
	ListenerProtocolTCP
	// ListenerProtocolHTTP is an HTTP listener.
	ListenerProtocolHTTP
	// ListenerProtocolAuto enables auto protocol detection
	ListenerProtocolAuto
)

// ModelProtocolToListenerProtocol converts from a config.Protocol to its corresponding plugin.ListenerProtocol
func ModelProtocolToListenerProtocol(p protocol.Instance) ListenerProtocol {
	switch p {
	case protocol.HTTP, protocol.HTTP2, protocol.HTTP_PROXY, protocol.GRPC, protocol.GRPCWeb:
		return ListenerProtocolHTTP
	case protocol.TCP, protocol.HTTPS, protocol.TLS,
		protocol.Mongo, protocol.Redis, protocol.MySQL:
		return ListenerProtocolTCP
	case protocol.UDP:
		return ListenerProtocolUnknown
	case protocol.Unsupported:
		return ListenerProtocolAuto
	default:
		// Should not reach here.
		return ListenerProtocolAuto
	}
}

type TransportProtocol uint8

const (
	// TransportProtocolTCP is a TCP listener
	TransportProtocolTCP = iota
	// TransportProtocolQUIC is a QUIC listener
	TransportProtocolQUIC
)

func (tp TransportProtocol) String() string {
	switch tp {
	case TransportProtocolTCP:
		return "tcp"
	case TransportProtocolQUIC:
		return "quic"
	}
	return "unknown"
}

func (tp TransportProtocol) ToEnvoySocketProtocol() core.SocketAddress_Protocol {
	if tp == TransportProtocolQUIC {
		return core.SocketAddress_UDP
	}
	return core.SocketAddress_TCP
}

// ListenerClass defines the class of the listener
type ListenerClass int

const (
	ListenerClassUndefined ListenerClass = iota
	ListenerClassSidecarInbound
	ListenerClassSidecarOutbound
	ListenerClassGateway
)
