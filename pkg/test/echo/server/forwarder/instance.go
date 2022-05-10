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

package forwarder

import (
	"context"
	"fmt"
	"io"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ io.Closer = &Instance{}

// Instance is a client for forwarding requests to echo servers.
type Instance struct {
	e           *executor
	protocolMap map[scheme.Instance]protocol
	protocols   []protocol
}

// New creates a new forwarder Instance.
func New() *Instance {
	var protocols []protocol
	add := func(p protocol) protocol {
		protocols = append(protocols, p)
		return p
	}

	// Create the protocols and populate the map.
	e := newExecutor()
	protocolMap := make(map[scheme.Instance]protocol)
	h := add(newHTTPProtocol(e))
	protocolMap[scheme.HTTP] = h
	protocolMap[scheme.HTTPS] = h
	protocolMap[scheme.DNS] = add(newDNSProtocol(e))
	protocolMap[scheme.GRPC] = add(newGRPCProtocol(e))
	protocolMap[scheme.WebSocket] = add(newWebsocketProtocol(e))
	protocolMap[scheme.TLS] = add(newTLSProtocol(e))
	protocolMap[scheme.XDS] = add(newXDSProtocol(e))
	protocolMap[scheme.TCP] = add(newTCPProtocol(e))

	return &Instance{
		e:           e,
		protocolMap: protocolMap,
		protocols:   protocols,
	}
}

// ForwardEcho sends the requests and collect the responses.
func (i *Instance) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	if err := cfg.fillDefaults(); err != nil {
		return nil, err
	}

	// Lookup the protocol.
	p := i.protocolMap[cfg.scheme]
	if p == nil {
		return nil, fmt.Errorf("no protocol handler found for scheme %s", cfg.scheme)
	}

	return p.ForwardEcho(ctx, cfg)
}

func (i *Instance) Close() error {
	i.e.Close()

	for _, p := range i.protocols {
		_ = p.Close()
	}

	return nil
}
