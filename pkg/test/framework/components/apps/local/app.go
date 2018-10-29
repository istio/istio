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
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"net"
	"net/url"
	"strconv"
	"testing"

	"istio.io/istio/pkg/test/framework/environments/local/service"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/framework/components/apps/local/agent"
	"istio.io/istio/pkg/test/framework/environment"
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

func newApp(cfg appConfig) (a environment.DeployedApp, err error) {
	newapp := &app{
		name: cfg.serviceName,
	}
	defer func() {
		if err != nil {
			_ = newapp.Close()
		}
	}()

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
	newapp.agent, err = agentFactory(cfg.serviceName, cfg.version, cfg.serviceManager, appFactory)
	if err != nil {
		return
	}

	// Create the endpoints for the app.
	var grpcEndpoint *endpoint
	ports := newapp.agent.GetPorts()
	endpoints := make([]environment.DeployedAppEndpoint, len(ports))
	for i, port := range ports {
		ep := &endpoint{
			owner: newapp,
			port:  port,
		}
		endpoints[i] = ep

		if ep.Protocol() == model.ProtocolGRPC {
			grpcEndpoint = ep
		}
	}
	newapp.endpoints = endpoints

	// Create the client for sending forward requests.
	if grpcEndpoint == nil {
		return nil, errors.New("unable to find grpc port for application")
	}
	newapp.client, err = echo.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcEndpoint.port.ApplicationPort))
	if err != nil {
		return nil, err
	}

	return newapp, nil
}

type app struct {
	name      string
	agent     agent.Agent
	endpoints []environment.DeployedAppEndpoint
	client    *echo.Client
}

// GetAgent is a utility method for testing that extracts the agent from a local app.
func GetAgent(a environment.DeployedApp) agent.Agent {
	localApp, ok := a.(*app)
	if !ok {
		return nil
	}
	return localApp.agent
}

func (a *app) Close() (err error) {
	if a.client != nil {
		err = a.client.Close()
	}
	if a.agent != nil {
		err = multierror.Append(err, a.agent.Close()).ErrorOrNil()
	}
	return
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

	// Forward a request from 'this' service to the destination service.
	dstURL := dst.makeURL(opts)
	dstServiceName := dst.owner.Name()
	resp, err := a.client.ForwardEcho(&proto.ForwardEchoRequest{
		Url:   dstURL.String(),
		Count: int32(opts.Count),
		Headers: []*proto.Header{
			{
				Key:   "Host",
				Value: dstServiceName,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(resp))
	}
	if !resp[0].IsOK() {
		return nil, fmt.Errorf("unexpected response status code: %s", resp[0].Code)
	}
	if resp[0].Host != dstServiceName {
		return nil, fmt.Errorf("unexpected host: %s", resp[0].Host)
	}
	if resp[0].Port != strconv.Itoa(dst.port.ApplicationPort) {
		return nil, fmt.Errorf("unexpected port: %s", resp[0].Port)
	}

	return resp, nil
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
