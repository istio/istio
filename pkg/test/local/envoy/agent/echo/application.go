//  Copyright 2018 Istio Authors
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

package echo

import (
	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/server/echo"
	"istio.io/istio/pkg/test/local/envoy/agent"
)

// PortConfig contains meta information about a port
type PortConfig struct {
	Name     string
	Protocol model.Protocol
}

// Factory is a factory for echo applications.
type Factory struct {
	Ports   []PortConfig
	TLSCert string
	TLSCKey string
	Version string
}

// NewApplication is an agent.ApplicationFactory function that manufactures echo applications.
func (f *Factory) NewApplication() (agent.Application, agent.StopFunc, error) {
	if err := f.validate(); err != nil {
		return nil, nil, err
	}

	app := &application{}
	server := &echo.Server{
		HTTPPorts: make([]int, f.getProtocolCount(model.ProtocolHTTP)),
		GRPCPorts: make([]int, f.getProtocolCount(model.ProtocolGRPC)),
		TLSCert:   f.TLSCert,
		TLSCKey:   f.TLSCKey,
		Version:   f.Version,
	}
	if err := server.Start(); err != nil {
		return nil, nil, err
	}
	app.ports = f.createPorts(server)

	stopFunc := func() error {
		if server != nil {
			return server.Stop()
		}
		return nil
	}
	return app, stopFunc, nil
}

func (f *Factory) validate() error {
	for _, port := range f.Ports {
		switch port.Protocol {
		case model.ProtocolHTTP:
		case model.ProtocolGRPC:
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}
	return nil
}

func (f *Factory) getProtocolCount(protocol model.Protocol) int {
	count := 0
	for _, p := range f.Ports {
		if p.Protocol == protocol {
			count++
		}
	}
	return count
}

func (f *Factory) createPorts(server *echo.Server) []model.Port {
	httpPorts := server.HTTPPorts
	grpcPorts := server.GRPCPorts
	out := make([]model.Port, len(httpPorts)+len(grpcPorts))
	httpIndex := 0
	grpcIndex := 0
	outIndex := 0
	for _, port := range f.Ports {
		switch port.Protocol {
		case model.ProtocolHTTP:
			out[outIndex] = model.Port{
				Name:     port.Name,
				Protocol: port.Protocol,
				Port:     httpPorts[httpIndex],
			}
			outIndex++
			httpIndex++
		case model.ProtocolGRPC:
			out[outIndex] = model.Port{
				Name:     port.Name,
				Protocol: port.Protocol,
				Port:     grpcPorts[grpcIndex],
			}
			outIndex++
			grpcIndex++
		}
	}

	return out
}

type application struct {
	ports []model.Port
}

// GetPorts implements the agent.Application interface
func (a *application) GetPorts() []model.Port {
	return a.ports
}
