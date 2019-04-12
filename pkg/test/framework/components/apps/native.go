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

package apps

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/apps/agent"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/environment/native/service"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond
)

var (
	_ Instance  = &nativeComponent{}
	_ io.Closer = &nativeComponent{}

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

type nativeComponent struct {
	id resource.ID

	//tlsCKey          string
	//tlsCert          string
	discoveryAddress *net.TCPAddr
	serviceManager   *service.Manager
	apps             []App
}

// NewNativeComponent factory function for the component
func newNative(ctx resource.Context, cfg Config) (Instance, error) {
	env := ctx.Environment().(*native.Environment)
	c := &nativeComponent{
		apps: make([]App, 0),
	}
	c.id = ctx.TrackResource(c)

	nativePilot, ok := cfg.Pilot.(pilot.Native)
	if !ok {
		return nil, errors.New("pilot does not support in-process interface")
	}

	//return NewApps(p.GetDiscoveryAddress(), e.ServiceManager)
	cfgs := []appConfig{
		{
			serviceName: "a",
			version:     "v1",
		},
		{
			serviceName: "b",
			version:     "unversioned",
		},
		{
			serviceName: "c",
			version:     "v1",
		},
		// TODO(nmittler): Investigate how to support multiple versions in the local environment.
		/*{
			serviceName: "c",
			version:     "v2",
		},*/
	}

	c.discoveryAddress = nativePilot.GetDiscoveryAddress()
	c.serviceManager = env.ServiceManager

	for _, cfg := range cfgs {
		//cfg.tlsCKey = c.tlsCert
		//cfg.tlsCert = c.tlsCert
		cfg.discoveryAddress = c.discoveryAddress
		cfg.serviceManager = c.serviceManager

		app, err := newNativeApp(cfg)
		if err != nil {
			return nil, err
		}

		c.apps = append(c.apps, app)
	}

	if err := c.waitForAppConfigDistribution(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *nativeComponent) ID() resource.ID {
	return c.id
}

func (c *nativeComponent) Namespace() namespace.Instance {
	return nil
}

// Close implements io.Closer
func (c *nativeComponent) Close() (err error) {
	for i, a := range c.apps {
		if a != nil {
			err = multierror.Append(err, a.(*nativeApp).Close()).ErrorOrNil()
			c.apps[i] = nil
		}
	}
	return
}

// GetApp implements Apps
func (c *nativeComponent) GetApp(name string) (App, error) {
	for _, a := range c.apps {
		if a.Name() == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("app %s does not exist", name)
}

// GetApp implements Apps
func (c *nativeComponent) GetAppOrFail(name string, t testing.TB) App {
	a, err := c.GetApp(name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (c *nativeComponent) waitForAppConfigDistribution() error {
	// Wait for config for all services to be distributed to all Envoys.
	endTime := time.Now().Add(timeout)
	for _, src := range c.apps {
		for _, target := range c.apps {
			if src == target {
				continue
			}
			for {
				err := src.(*nativeApp).agent.CheckConfiguredForService(target.(*nativeApp).agent)
				if err == nil {
					break
				}

				if time.Now().After(endTime) {
					out := fmt.Sprintf("failed to configure apps: %v. Dumping Envoy configurations:\n", err)
					for _, a := range c.apps {
						dump, _ := configDumpStr(a)
						out += fmt.Sprintf("app %s Config: %s\n", a.Name(), dump)
					}

					return errors.New(out)
				}
				time.Sleep(retryInterval)
			}
		}
	}
	return nil
}

func configDumpStr(a App) (string, error) {
	return envoy.GetConfigDumpStr(a.(*nativeApp).agent.GetAdminPort())
}

// ConstructDiscoveryRequest returns an Envoy discovery request.
func ConstructDiscoveryRequest(a App, typeURL string) *xdsapi.DiscoveryRequest {
	nodeID := agent.GetNodeID(a.(*nativeApp).agent)
	return &xdsapi.DiscoveryRequest{
		Node: &core.Node{
			Id: nodeID,
		},
		TypeUrl: typeURL,
	}
}

type appConfig struct {
	serviceName      string
	version          string
	tlsCKey          string
	tlsCert          string
	discoveryAddress *net.TCPAddr
	serviceManager   *service.Manager
}

func newNativeApp(cfg appConfig) (a App, err error) {
	newapp := &nativeApp{
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
	newapp.agent, err = agentFactory(cfg.serviceName, cfg.version, cfg.serviceManager, appFactory, nil)
	if err != nil {
		return
	}

	// Create the endpoints for the app.
	var grpcEndpoint *nativeEndpoint
	ports := newapp.agent.GetPorts()
	endpoints := make([]AppEndpoint, len(ports))
	for i, port := range ports {
		ep := &nativeEndpoint{
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

type nativeApp struct {
	name      string
	agent     agent.Agent
	endpoints []AppEndpoint
	client    *echo.Client
}

func (a *nativeApp) Close() (err error) {
	if a.client != nil {
		err = a.client.Close()
	}
	if a.agent != nil {
		err = multierror.Append(err, a.agent.Close()).ErrorOrNil()
	}
	return
}

func (a *nativeApp) Name() string {
	return a.name
}

func (a *nativeApp) Endpoints() []AppEndpoint {
	return a.endpoints
}

func (a *nativeApp) EndpointsForProtocol(protocol model.Protocol) []AppEndpoint {
	eps := make([]AppEndpoint, 0, len(a.endpoints))
	for _, ep := range a.endpoints {
		if ep.Protocol() == protocol {
			eps = append(eps, ep)
		}
	}
	return eps
}

func (a *nativeApp) Call(e AppEndpoint, opts AppCallOptions) ([]*echo.ParsedResponse, error) {
	dst, ok := e.(*nativeEndpoint)
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

func (a *nativeApp) CallOrFail(e AppEndpoint, opts AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const nativeEndpointIP = "127.0.0.1"

type nativeEndpoint struct {
	owner *nativeApp
	port  *agent.MappedPort
}

func (e *nativeEndpoint) Name() string {
	return e.port.Name
}

func (e *nativeEndpoint) Owner() App {
	return e.owner
}

func (e *nativeEndpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

func (e *nativeEndpoint) NetworkEndpoint() model.NetworkEndpoint {
	return model.NetworkEndpoint{
		Address: nativeEndpointIP,
		Port:    e.port.ApplicationPort,
		ServicePort: &model.Port{
			Name:     e.port.Name,
			Port:     e.port.ProxyPort,
			Protocol: e.port.Protocol,
		},
	}
}

func (e *nativeEndpoint) makeURL(opts AppCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case AppProtocolHTTP:
	case AppProtocolGRPC:
	case AppProtocolWebSocket:
	default:
		protocol = string(AppProtocolHTTP)
	}

	if opts.Secure {
		protocol += "s"
	}

	host := nativeEndpointIP
	port := e.port.ProxyPort
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
}
