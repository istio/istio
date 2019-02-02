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

package echo

import (
	gocontext "context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
)

var (
	_ components.Echo = &nativeComponent{}
	_ api.Component   = &nativeComponent{}
	_ io.Closer       = &nativeComponent{}

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

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{}, nil
}

type nativeComponent struct {
	name      string
	scope     lifecycle.Scope
	endpoints []components.EchoEndpoint
	client    *echo.Client
}

type echoConfig struct {
	serviceName string
	version     string
}

func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.Echo
}

func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// Start implements the api.Component interface
func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	// Setup a close function to close the echo instance on an error.
	defer func() {
		if err != nil {
			c.Close()
		}
	}()

	// TODO(sven): Pass configuration through Start() and use that for service name and version.
	c.name = "a"

	cfg := echoConfig{
		serviceName: c.name,
		version:     "v1",
	}

	echoFactory := (&echo.Factory{
		Ports:   ports,
		Version: cfg.version,
	}).NewApplication

	dialer := application.Dialer{
		GRPC:      c.dialGRPC,
		Websocket: c.dialWebsocket,
		HTTP:      c.doHTTP,
	}
	app, err := echoFactory(dialer)

	// Create the endpoints for the app.
	var grpcEndpoint *nativeEndpoint
	ports := app.GetPorts()
	endpoints := make([]components.EchoEndpoint, len(ports))
	for i, port := range ports {
		ep := &nativeEndpoint{
			owner: c,
			port:  port,
		}
		endpoints[i] = ep

		if ep.Protocol() == model.ProtocolGRPC {
			grpcEndpoint = ep
		}
	}
	c.endpoints = endpoints

	// Create the client for sending forward requests.
	if grpcEndpoint == nil {
		return errors.New("unable to find grpc port for application")
	}
	c.client, err = echo.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcEndpoint.port.Port))
	if err != nil {
		return err
	}
	return
}

// function for establishing GRPC connections from the application.
func (c *nativeComponent) dialGRPC(ctx gocontext.Context, address string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, address, opts...)
}

// function for establishing Websocket connections from the application.
func (c *nativeComponent) dialWebsocket(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error) {
	return dialer.Dial(urlStr, requestHeader)
}

// function for making outbound HTTP requests from the application.
func (c *nativeComponent) doHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// Close implements io.Closer
func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = c.client.Close()
	}
	return
}

func (c *nativeComponent) Name() string {
	return c.name
}

func (c *nativeComponent) Endpoints() []components.EchoEndpoint {
	return c.endpoints
}

func (c *nativeComponent) EndpointsForProtocol(protocol model.Protocol) []components.EchoEndpoint {
	eps := make([]components.EchoEndpoint, 0, len(c.endpoints))
	for _, ep := range c.endpoints {
		if ep.Protocol() == protocol {
			eps = append(eps, ep)
		}
	}
	return eps
}

func (c *nativeComponent) Call(ee components.EchoEndpoint, opts components.EchoCallOptions) ([]*echo.ParsedResponse, error) {
	dst, ok := ee.(*nativeEndpoint)
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

	var headers []*proto.Header
	headers = append(headers, &proto.Header{Key: "Host", Value: dstServiceName})
	for key, values := range opts.Headers {
		for _, value := range values {
			headers = append(headers, &proto.Header{Key: key, Value: value})
		}
	}
	request := &proto.ForwardEchoRequest{
		Url:     dstURL.String(),
		Count:   int32(opts.Count),
		Headers: headers,
	}

	resp, err := c.client.ForwardEcho(request)
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
		return nil, fmt.Errorf("unexpected host: %s (expected %s)", resp[0].Host, dstServiceName)
	}
	if resp[0].Port != strconv.Itoa(dst.port.Port) {
		return nil, fmt.Errorf("unexpected port: %s (expected %s)", resp[0].Port, strconv.Itoa(dst.port.Port))
	}

	return resp, nil
}

func (c *nativeComponent) CallOrFail(ee components.EchoEndpoint, opts components.EchoCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := c.Call(ee, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type nativeEndpoint struct {
	owner *nativeComponent
	port  *model.Port
}

func (e *nativeEndpoint) Name() string {
	return e.port.Name
}

func (e *nativeEndpoint) Owner() components.Echo {
	return e.owner
}

func (e *nativeEndpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

func (e *nativeEndpoint) makeURL(opts components.EchoCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case components.AppProtocolHTTP:
	case components.AppProtocolGRPC:
	case components.AppProtocolWebSocket:
	default:
		protocol = string(components.AppProtocolHTTP)
	}

	if opts.Secure {
		protocol += "s"
	}

	host := "127.0.0.1"
	port := e.port.Port
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
}
