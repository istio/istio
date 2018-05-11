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

package agent

import (
	"fmt"
	"net"

	"go.uber.org/multierr"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/server/echo"
	"istio.io/istio/pkg/test/local/proxy/envoy"
)

// PortConfig contains meta information about a port
type PortConfig struct {
	Name     string
	Protocol model.Protocol
}

// Config represents the configuration for an Agent
type Config struct {
	ServiceName string
	Ports       []PortConfig
	TLSCert     string
	TLSCKey     string
	Version     string
	TmpDir      string
}

// Port contains the port mapping for a single configured port
type Port struct {
	Config      PortConfig
	ProxyPort   int
	ServicePort int
}

// Agent bootstraps a local service/Envoy combination.
type Agent struct {
	Config Config
	proxy  *envoy.Envoy
	// TODO(nmittler): Abstract out an interface for a backend service needed by this agent.
	service        *echo.Server
	proxyConfig    *proxyConfig
	proxyAdminPort int
	ports          []Port
}

// Start starts Envoy and the service.
func (a *Agent) Start() (err error) {
	if err = a.startService(); err != nil {
		return err
	}

	// Generate the port mappings between Envoy and the backend service.
	a.proxyAdminPort, a.ports, err = a.createPorts()
	if err != nil {
		return err
	}

	return a.startEnvoy()
}

// Stop stops Envoy and the service.
func (a *Agent) Stop() error {
	err := a.stopService()
	return multierr.Append(err, a.stopEnvoy())
}

// GetPorts returns the list of runtime ports after the Agent has been started.
func (a *Agent) GetPorts() []Port {
	return a.ports
}

// GetEnvoyAdminPort returns the admin port for Envoy after the Agent has been started.
func (a *Agent) GetEnvoyAdminPort() int {
	return a.proxyAdminPort
}

func (a *Agent) startService() error {
	// TODO(nmittler): Add support for other protocols
	for _, port := range a.Config.Ports {
		switch port.Protocol {
		case model.ProtocolHTTP:
			// Just verifying that all ports are HTTP for now.
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}

	a.service = &echo.Server{
		HTTPPorts: make([]int, len(a.Config.Ports)),
		TLSCert:   a.Config.TLSCert,
		TLSCKey:   a.Config.TLSCKey,
		Version:   a.Config.Version,
	}
	return a.service.Start()
}

func (a *Agent) stopService() error {
	if a.service != nil {
		return a.service.Stop()
	}
	return nil
}

func (a *Agent) startEnvoy() (err error) {
	// Create the configuration object
	a.proxyConfig, err = (&proxyConfigBuilder{
		ServiceName: a.Config.ServiceName,
		AdminPort:   a.proxyAdminPort,
		Ports:       a.ports,
		tmpDir:      a.Config.TmpDir,
	}).build()
	if err != nil {
		return err
	}

	// Create and start envoy with the configuration
	a.proxy = &envoy.Envoy{
		YamlFile: a.proxyConfig.yamlFile,
	}
	return a.proxy.Start()
}

func (a *Agent) stopEnvoy() (err error) {
	if a.proxy != nil {
		err = a.proxy.Stop()
	}
	if a.proxyConfig != nil {
		a.proxyConfig.dispose()
		a.proxyConfig = nil
	}
	return err
}

func (a *Agent) createPorts() (adminPort int, ports []Port, err error) {
	if adminPort, err = findFreePort(); err != nil {
		return
	}

	servicePorts := a.service.HTTPPorts
	ports = make([]Port, len(servicePorts))
	for i, servicePort := range servicePorts {
		var envoyPort int
		envoyPort, err = findFreePort()
		if err != nil {
			return
		}

		ports[i] = Port{
			Config:      a.Config.Ports[i],
			ServicePort: servicePort,
			ProxyPort:   envoyPort,
		}
	}
	return
}

func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
