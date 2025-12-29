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

import "istio.io/istio/pkg/config/protocol"

// TLSSettings defines TLS configuration for Echo server
type TLSSettings struct {
	// If not empty, RootCert supplies the extra root cert that will be appended to the system cert pool.
	RootCert   string
	ClientCert string
	Key        string
	// If provided, override the host name used for the connection
	// This needed for integration tests, as we are connecting using a port-forward (127.0.0.1), so
	// any DNS certs will not validate.
	Hostname string
	// If set to true, the cert will be provisioned by proxy, and extra cert volume will be mounted.
	ProxyProvision bool
	// AcceptAnyALPN, if true, will make the server accept ANY ALPNs. This comes at the expense of
	// allowing h2 negotiation and being able to detect the negotiated ALPN (as there is none), because
	// Golang doesn't like us doing this (https://github.com/golang/go/issues/46310).
	// This is useful when the server is simulating Envoy which does unconventional things with ALPN.
	AcceptAnyALPN bool
}

// Port represents a network port where a service is listening for
// connections. The port should be annotated with the type of protocol
// used by the port.
type Port struct {
	// Name ascribes a human readable name for the port object. When a
	// service has multiple ports, the name field is mandatory
	Name string

	// Port number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service.
	Port int

	// Protocol to be used for the port.
	Protocol protocol.Instance

	// TLS determines if the port will use TLS.
	TLS bool

	// RequireClientCert determines if the port will be mTLS.
	RequireClientCert bool

	// EndpointPicker indicates this port should serve as an endpoint picker (ext_proc gRPC service).
	// Only valid when Protocol is GRPC.
	EndpointPicker bool

	// ServerFirst if a port will be server first
	ServerFirst bool

	// InstanceIP determines if echo will listen on the instance IP, or wildcard
	InstanceIP bool

	// LocalhostIP determines if echo will listen on the localhost IP; otherwise, it will listen on wildcard
	LocalhostIP bool

	// XDSServer, for gRPC servers, will use the xds.NewGRPCServer constructor to rely on XDS configuration.
	// If this flag is set but the environment variable feature gates aren't, we should fail due to gRPC internals.
	XDSServer bool

	// XDSTestBootstrap allows settings per-endpoint bootstrap without using the GRPC_XDS_BOOTSTRAP env var
	XDSTestBootstrap []byte

	// XDSReadinessTLS determines if the XDS server should expect a TLS server, used for readiness probes
	XDSReadinessTLS bool

	// ProxyProtocol indicates this listener should terminate HA PROXY protocol (v1 or v2)
	ProxyProtocol bool
}

// PortList is a set of ports
type PortList []*Port

var ServerFirstMagicString = "server-first-protocol\n"
