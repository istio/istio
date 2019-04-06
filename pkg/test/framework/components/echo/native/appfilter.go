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

package native

import (
	"context"
	"net"
	"net/http"
	netUrl "net/url"
	"strconv"

	"github.com/gorilla/websocket"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo"
)

// newAppFilter creates a wrapper around the echo application that modifies ports on
// outbound requests in order to route them through Envoy.
func newAppFilter(filter discoveryFilter, factory *echo.Factory) (application.Application, error) {
	a := &appFilterImpl{
		filter: filter,
	}

	dialer := application.Dialer{
		GRPC:      a.dialGRPC,
		Websocket: a.dialWebsocket,
		HTTP:      a.doHTTP,
	}

	var err error
	a.Application, err = factory.NewApplication(dialer)
	if err != nil {
		return nil, err
	}
	return a, nil
}

type appFilterImpl struct {
	application.Application
	filter discoveryFilter
}

// function for establishing GRPC connections from the application.
func (a *appFilterImpl) dialGRPC(ctx context.Context, address string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// Modify the outbound URL being created by the application
	address = a.modifyClientURLString(address)
	return grpc.DialContext(ctx, address, opts...)
}

// function for establishing Websocket connections from the application.
func (a *appFilterImpl) dialWebsocket(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error) {
	// Modify the outbound URL being created by the application
	urlStr = a.modifyClientURLString(urlStr)
	return dialer.Dial(urlStr, requestHeader)
}

// function for making outbound HTTP requests from the application.
func (a *appFilterImpl) doHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	a.modifyClientURL(req.URL)
	return client.Do(req)
}

func (a *appFilterImpl) modifyClientURLString(url string) string {
	parsedURL, err := netUrl.Parse(url)
	if err != nil {
		// Failed to parse the URL, just use the original.
		return url
	}
	a.modifyClientURL(parsedURL)
	return parsedURL.String()
}

func (a *appFilterImpl) modifyClientURL(url *netUrl.URL) {
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		// No port was specified. Nothing to do.
		return
	}

	boundPort, ok := a.filter.GetBoundOutboundListenerPort(port)
	if ok {
		url.Host = net.JoinHostPort(localhost, strconv.Itoa(boundPort))
	}
}
