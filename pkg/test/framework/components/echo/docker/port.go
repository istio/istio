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

package docker

import (
	"errors"
	"strconv"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/docker"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/reserveport"
)

type portMap struct {
	ports         []port
	hostAgentPort uint16
}

func newPortMap(portMgr reserveport.PortManager, cfg echo.Config) (*portMap, error) {
	m := &portMap{}

	// Reserve a status port for the host agent.
	var err error
	if m.hostAgentPort, err = portMgr.ReservePortNumber(); err != nil {
		return nil, err
	}

	hasHTTP := false
	hasGRPC := false
	for _, p := range cfg.Ports {
		// Reserve a host port.
		hostPort, err := portMgr.ReservePortNumber()
		if err != nil {
			return nil, err
		}

		m.ports = append(m.ports, port{
			containerPort: p,
			hostPort:      hostPort,
		})

		switch p.Protocol {
		case protocol.HTTP:
			hasHTTP = true
		case protocol.GRPC, protocol.GRPCWeb:
			hasGRPC = true
		}
	}

	if !hasHTTP {
		return nil, errors.New("unable to find http port for application")
	}

	if !hasGRPC {
		return nil, errors.New("unable to find grpc port for application")
	}

	return m, nil
}

func (m *portMap) toEchoArgs() []string {
	echoArgs := make([]string, 0)
	for _, port := range m.ports {
		portNumber := port.containerPort.ServicePort
		if port.containerPort.Protocol.IsGRPC() {
			echoArgs = append(echoArgs, "--grpc", strconv.Itoa(portNumber))
		} else {
			echoArgs = append(echoArgs, "--port", strconv.Itoa(portNumber))
		}
	}
	return echoArgs
}

func (m *portMap) toDocker() docker.PortMap {
	portMap := docker.PortMap{
		// Add an entry for the agent status port.
		docker.ContainerPort(agentStatusPort): docker.HostPort(m.hostAgentPort),
	}

	for _, port := range m.ports {
		portMap[docker.ContainerPort(port.containerPort.ServicePort)] = docker.HostPort(port.hostPort)
	}
	return portMap
}

func (m *portMap) http() port {
	for _, port := range m.ports {
		if port.containerPort.Protocol == protocol.HTTP {
			return port
		}
	}
	panic("unable to find http port for Echo application")
}

func (m *portMap) grpc() port {
	for _, port := range m.ports {
		if port.containerPort.Protocol.IsGRPC() {
			return port
		}
	}
	panic("unable to find grpc port for Echo application")
}

type port struct {
	containerPort echo.Port

	// The reserved port on the host that forwards to the container port.
	hostPort uint16
}
