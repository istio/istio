//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package components

import (
	"net/http"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// EchoProtocol enumerates the protocol options for calling an EchoEndpoint endpoint.
type EchoProtocol string

const (
	// EchoProtocolHTTP calls echo with HTTP
	EchoProtocolHTTP = "http"
	// EchoProtocolGRPC calls echo with GRPC
	EchoProtocolGRPC = "grpc"
	// EchoProtocolWebSocket calls echo with WebSocket
	EchoProtocolWebSocket = "ws"
)

// Echo is a component that provides access to the deployed echo service.
type Echo interface {
	component.Instance

	Name() string
	Endpoints() []EchoEndpoint
	EndpointsForProtocol(protocol model.Protocol) []EchoEndpoint
	Call(e EchoEndpoint, opts EchoCallOptions) ([]*echo.ParsedResponse, error)
	CallOrFail(e EchoEndpoint, opts EchoCallOptions, t testing.TB) []*echo.ParsedResponse
}

// EchoCallOptions defines options for calling a EchoEndpoint.
type EchoCallOptions struct {
	// Secure indicates whether a secure connection should be established to the endpoint.
	Secure bool

	// Protocol indicates the protocol to be used.
	Protocol EchoProtocol

	// UseShortHostname indicates whether shortened hostnames should be used. This may be ignored by the environment.
	UseShortHostname bool

	// Count indicates the number of exchanges that should be made with the service endpoint. If not set (i.e. 0), defaults to 1.
	Count int

	// Headers indicates headers that should be sent in the request. Ingnored for WebSocket calls.
	Headers http.Header
}

// EchoEndpoint represents a single endpoint in an Echo instance.
type EchoEndpoint interface {
	Name() string
	Owner() Echo
	Protocol() model.Protocol
}

// Get an echo instance from the repository.
func GetEcho(name string, e component.Repository, t testing.TB) Echo {
	return e.GetComponentOrFail(name, ids.Echo, t).(Echo)
}
