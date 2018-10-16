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

package local

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"testing"

	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/framework/components/apps/local/agent"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/local/service"
)

var (
	ports = model.PortList{
		{
			Name:     "http",
			Protocol: model.ProtocolHTTP,
		},
		{
			Name:     "http-two",
			Protocol: model.ProtocolHTTP,
		},
		{
			Name:     "tcp",
			Protocol: model.ProtocolTCP,
		},
		{
			Name:     "https",
			Protocol: model.ProtocolHTTPS,
		},
		{
			Name:     "http2-example",
			Protocol: model.ProtocolHTTP2,
		},
		{
			Name:     "grpc",
			Protocol: model.ProtocolGRPC,
		},
	}
)

type appConfig struct {
	serviceName      string
	version          string
	tlsCKey          string
	tlsCert          string
	discoveryAddress *net.TCPAddr
	serviceManager   *service.Manager
}

func newApp(cfg appConfig) (environment.DeployedApp, error) {
	appFactory := (&echo.Factory{
		Ports:   ports,
		Version: cfg.version,
		TLSCKey: cfg.tlsCKey,
		TLSCert: cfg.tlsCert,
	}).NewApplication

	agentFactory := (&agent.PilotAgentFactory{
		DiscoveryAddress: cfg.discoveryAddress,
	}).NewAgent

	// Create and start the agent.
	a, err := agentFactory(cfg.serviceName, cfg.version, cfg.serviceManager, appFactory)
	if err != nil {
		return nil, err
	}

	newapp := &app{
		name:  cfg.serviceName,
		agent: a,
	}

	var commandEndpoint *endpoint
	ports := a.GetPorts()
	endpoints := make([]environment.DeployedAppEndpoint, len(ports))
	for i, port := range ports {
		ep := &endpoint{
			owner: newapp,
			port:  port,
		}
		endpoints[i] = ep

		if ep.Protocol() == model.ProtocolGRPC {
			commandEndpoint = ep
		}
	}
	newapp.endpoints = endpoints
	newapp.commandEndpoint = commandEndpoint

	return newapp, nil
}

type app struct {
	name            string
	agent           agent.Agent
	endpoints       []environment.DeployedAppEndpoint
	commandEndpoint *endpoint
}

// GetAgent is a utility method for testing that extracts the agent from a local app.
func GetAgent(a environment.DeployedApp) agent.Agent {
	localApp, ok := a.(*app)
	if !ok {
		return nil
	}
	return localApp.agent
}

func (a *app) Close() error {
	return a.agent.Close()
}

func (a *app) Name() string {
	return a.name
}

func (a *app) Endpoints() []environment.DeployedAppEndpoint {
	return a.endpoints
}

func (a *app) EndpointsForProtocol(protocol model.Protocol) []environment.DeployedAppEndpoint {
	eps := make([]environment.DeployedAppEndpoint, 0, len(a.endpoints))
	for _, ep := range a.endpoints {
		if ep.Protocol() == protocol {
			eps = append(eps, ep)
		}
	}
	return eps
}

func (a *app) Call(e environment.DeployedAppEndpoint, opts environment.AppCallOptions) ([]*echo.ParsedResponse, error) {
	dst, ok := e.(*endpoint)
	if !ok {
		return nil, fmt.Errorf("supplied endpoint was not created by this environment")
	}

	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// Connect to the GRPC (command) endpoint of 'this' app.
	commandPort := a.commandEndpoint.port.ApplicationPort
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", commandPort), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Forward a request from 'this' service to the destination service.
	client := proto.NewEchoTestServiceClient(conn)
	dstURL := dst.makeURL(opts)
	dstServiceName := dst.owner.Name()
	resp, err := client.ForwardEcho(context.Background(), &proto.ForwardEchoRequest{
		Url:   dstURL.String(),
		Count: int32(opts.Count),
		Header: &proto.Header{
			Key:   "Host",
			Value: dstServiceName,
		},
	})
	if err != nil {
		return nil, err
	}

	parsedResponses := echo.ParseForwardedResponse(resp)
	if len(parsedResponses) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(parsedResponses))
	}
	if !parsedResponses[0].IsOK() {
		return nil, fmt.Errorf("unexpected response status code: %s", parsedResponses[0].Code)
	}
	if parsedResponses[0].Host != dstServiceName {
		return nil, fmt.Errorf("unexpected host: %s", parsedResponses[0].Host)
	}
	if parsedResponses[0].Port != strconv.Itoa(dst.port.ApplicationPort) {
		return nil, fmt.Errorf("unexpected port: %s", parsedResponses[0].Port)
	}

	return parsedResponses, nil
}

func (a *app) CallOrFail(e environment.DeployedAppEndpoint, opts environment.AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type endpoint struct {
	owner *app
	port  *agent.MappedPort
}

func (e *endpoint) Name() string {
	return e.port.Name
}

func (e *endpoint) Owner() environment.DeployedApp {
	return e.owner
}

func (e *endpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

func (e *endpoint) makeURL(opts environment.AppCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case environment.AppProtocolHTTP:
	case environment.AppProtocolGRPC:
	case environment.AppProtocolWebSocket:
	default:
		protocol = string(environment.AppProtocolHTTP)
	}

	if opts.Secure {
		protocol += "s"
	}

	host := "127.0.0.1"
	port := e.port.ProxyPort
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
}
