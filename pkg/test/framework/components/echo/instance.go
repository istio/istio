// Copyright 2019 Istio Authors
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
	"net/http"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

// Protocol enumerates the protocol options for calling an Endpoint endpoint.
type Protocol string

const (
	// EchoProtocolHTTP calls echo with HTTP
	EchoProtocolHTTP Protocol = "http"
	// EchoProtocolGRPC calls echo with GRPC
	EchoProtocolGRPC Protocol = "grpc"
	// EchoProtocolWebSocket calls echo with WebSocket
	EchoProtocolWebSocket Protocol = "ws"
)

// Instance is a component that provides access to the deployed echo service.
type Instance interface {
	resource.Resource

	// Config returns the configuration of the Echo instance.
	Config() Config

	// Endpoints returns the endpoints that are available for calling the Echo instance.
	Endpoints() []Endpoint

	// EndpointsForProtocol return the endpoints filtered for a specific protocol (e.g. GRPC)
	EndpointsForProtocol(protocol model.Protocol) []Endpoint

	// Call makes a call from this Echo instance to an Endpoint from another instance.
	Call(e Endpoint, opts CallOptions) ([]*echo.ParsedResponse, error)
	CallOrFail(e Endpoint, opts CallOptions, t testing.TB) []*echo.ParsedResponse
}

// Config defines the options for creating an Echo component.
type Config struct {
	// Service indicates the service name of the Echo application.
	Service string

	// Version indicates the version path for calls to the Echo application.
	Version string
}

// String implements the Configuration interface (which implements fmt.Stringer)
func (ec Config) String() string {
	return fmt.Sprint("{service: ", ec.Service, ", version: ", ec.Version, "}")
}

// CallOptions defines options for calling a Endpoint.
type CallOptions struct {
	// Secure indicates whether a secure connection should be established to the endpoint.
	Secure bool

	// Protocol indicates the protocol to be used.
	Protocol Protocol

	// UseShortHostname indicates whether shortened hostnames should be used. This may be ignored by the environment.
	UseShortHostname bool

	// Count indicates the number of exchanges that should be made with the service endpoint. If not set (i.e. 0), defaults to 1.
	Count int

	// Headers indicates headers that should be sent in the request. Ingnored for WebSocket calls.
	Headers http.Header
}

// Endpoint represents a single endpoint in an Echo instance.
type Endpoint interface {
	Name() string
	Owner() Instance
	Protocol() model.Protocol
}

// New returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i, err = newNative(ctx, cfg)
	})
	return
}

// NewOrFail returns a new instance of echo, or fails t if there is an error.
func NewOrFail(ctx resource.Context, t *testing.T, cfg Config) Instance {
	t.Helper()
	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("echo.NewOrFail: %v", err)
	}

	return i
}
