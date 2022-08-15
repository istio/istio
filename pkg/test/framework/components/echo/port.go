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
	"reflect"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
)

// NoServicePort defines the ServicePort value for a Port that is a workload-only port.
const NoServicePort = -1

// Port exposed by an Echo Instance
type Port struct {
	// Name of this port
	Name string

	// Protocol to be used for the port.
	Protocol protocol.Instance

	// ServicePort number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service. If zero (default), a service port will be automatically generated for this port.
	// If set to NoServicePort, this port will be assumed to be a workload-only port.
	ServicePort int

	// WorkloadPort number where the workload is listening for connections.
	// This need not be the same as the ServicePort where the service is accessed.
	WorkloadPort int

	// TLS determines whether the connection will be plain text or TLS. By default this is false (plain text).
	TLS bool

	// ServerFirst determines whether the port will use server first communication, meaning the client will not send the first byte.
	ServerFirst bool

	// InstanceIP determines if echo will listen on the instance IP; otherwise, it will listen on wildcard
	InstanceIP bool

	// LocalhostIP determines if echo will listen on the localhost IP; otherwise, it will listen on wildcard
	LocalhostIP bool
}

// IsWorkloadOnly returns true if there is no service port specified for this Port.
func (p Port) IsWorkloadOnly() bool {
	return p.ServicePort == NoServicePort
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

type Ports []Port

func (ps Ports) Contains(p Port) bool {
	for _, port := range ps {
		if reflect.DeepEqual(port, p) {
			return true
		}
	}
	return false
}

// ForName returns the first port found with the given name.
func (ps Ports) ForName(name string) (Port, bool) {
	for _, port := range ps {
		if name == port.Name {
			return port, true
		}
	}
	return Port{}, false
}

// MustForName calls ForName and panics if not found.
func (ps Ports) MustForName(name string) Port {
	p, found := ps.ForName(name)
	if !found {
		panic("port does not exist for name " + name)
	}
	return p
}

// ForProtocol returns the first port found with the given protocol.
func (ps Ports) ForProtocol(protocol protocol.Instance) (Port, bool) {
	for _, p := range ps {
		if p.Protocol == protocol {
			return p, true
		}
	}
	return Port{}, false
}

// ForServicePort returns the first port found with the given service port.
func (ps Ports) ForServicePort(port int) (Port, bool) {
	for _, p := range ps {
		if p.ServicePort == port {
			return p, true
		}
	}
	return Port{}, false
}

// MustForProtocol calls ForProtocol and panics if not found.
func (ps Ports) MustForProtocol(protocol protocol.Instance) Port {
	p, found := ps.ForProtocol(protocol)
	if !found {
		panic("port does not exist for protocol " + protocol)
	}
	return p
}

// GetServicePorts returns the subset of ports that contain a service port.
func (ps Ports) GetServicePorts() Ports {
	out := make(Ports, 0, len(ps))
	for _, p := range ps {
		if !p.IsWorkloadOnly() {
			out = append(out, p)
		}
	}
	return out
}

// GetWorkloadOnlyPorts returns the subset of ports that do not contain a service port.
func (ps Ports) GetWorkloadOnlyPorts() Ports {
	out := make(Ports, 0, len(ps))
	for _, p := range ps {
		if p.IsWorkloadOnly() {
			out = append(out, p)
		}
	}
	return out
}
