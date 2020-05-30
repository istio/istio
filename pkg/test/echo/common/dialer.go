//  Copyright Istio Authors
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

package common

import (
	"context"
	"net"
	"net/http"

	"github.com/gorilla/websocket"

	"google.golang.org/grpc"
)

var (
	// DefaultGRPCDialFunc just calls grpc.Dial directly, with no alterations to the arguments.
	DefaultGRPCDialFunc = grpc.DialContext
	// DefaultWebsocketDialFunc just calls dialer.Dial, with no alterations to the arguments.
	DefaultWebsocketDialFunc = func(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error) {
		return dialer.Dial(urlStr, requestHeader)
	}
	// DefaultHTTPDoFunc just calls client.Do with no alterations to the arguments.
	DefaultHTTPDoFunc = func(client *http.Client, req *http.Request) (*http.Response, error) {
		return client.Do(req)
	}
	// DefaultTCPDialFunc just calls dialer.Dial, with no alterations to the arguments.
	DefaultTCPDialFunc = func(dialer net.Dialer, ctx context.Context, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", address)
	}
	// DefaultDialer is provides defaults for all dial functions.
	DefaultDialer = Dialer{
		GRPC:      DefaultGRPCDialFunc,
		Websocket: DefaultWebsocketDialFunc,
		HTTP:      DefaultHTTPDoFunc,
		TCP:       DefaultTCPDialFunc,
	}
)

// GRPCDialFunc a function for establishing a GRPC connection.
type GRPCDialFunc func(ctx context.Context, address string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// WebsocketDialFunc a function for establishing a Websocket connection.
type WebsocketDialFunc func(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error)

// HTTPDoFunc a function for executing an HTTP request.
type HTTPDoFunc func(client *http.Client, req *http.Request) (*http.Response, error)

// TCPDialFunc a function for establishing a TCP connection.
type TCPDialFunc func(dialer net.Dialer, ctx context.Context, address string) (net.Conn, error)

// Dialer is a replaceable set of functions for creating client-side connections for various protocols, allowing a test
// application to intercept the connection creation.
type Dialer struct {
	GRPC      GRPCDialFunc
	Websocket WebsocketDialFunc
	HTTP      HTTPDoFunc
	TCP       TCPDialFunc
}

// FillInDefaults fills in any missing dial functions with defaults
func (d Dialer) FillInDefaults() Dialer {
	ret := DefaultDialer

	if d.GRPC != nil {
		ret.GRPC = d.GRPC
	}
	if d.Websocket != nil {
		ret.Websocket = d.Websocket
	}
	if d.HTTP != nil {
		ret.HTTP = d.HTTP
	}
	if d.TCP != nil {
		ret.TCP = d.TCP
	}
	return ret
}
