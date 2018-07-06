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

package protocol

import (
	"net/http"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

var (
	// DefaultGRPCDialFunc just calls grpc.Dial directly, with no alterations to the arguments.
	DefaultGRPCDialFunc = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return grpc.Dial(target, opts...)
	}
	// DefaultWebsocketDialFunc just calls dialer.Dial, with no alterations to the arguments.
	DefaultWebsocketDialFunc = func(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error) {
		return dialer.Dial(urlStr, requestHeader)
	}
	// DefaultHTTPDoFunc just calls client.Do with no alterations to the arguments.
	DefaultHTTPDoFunc = func(client *http.Client, req *http.Request) (*http.Response, error) {
		return client.Do(req)
	}
	// DefaultClient is provides defaults for all functions.
	DefaultClient = Client{
		GRPC: GRPCClient{
			Dial: DefaultGRPCDialFunc,
		},
		Websocket: WebsocketClient{
			Dial: DefaultWebsocketDialFunc,
		},
		HTTP: HTTPClient{
			Do: DefaultHTTPDoFunc,
		},
	}
)

// GRPCDialFunc a function for establishing a GRPC connection.
type GRPCDialFunc func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// WebsocketDialFunc a function for establishing a Websocket connection.
type WebsocketDialFunc func(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error)

// HTTPDoFunc a function for executing an HTTP request.
type HTTPDoFunc func(client *http.Client, req *http.Request) (*http.Response, error)

// Client is a replaceable set of functions for creating client-side connections for various protocols, allowing a test
// application to intercept the connection creation.
type Client struct {
	GRPC      GRPCClient
	Websocket WebsocketClient
	HTTP      HTTPClient
}

// GRPCClient is a replaceable function for establishing a gRPC connection.
type GRPCClient struct {
	Dial GRPCDialFunc
}

// WebsocketClient is a replaceable function for establishing a Websocket connection.
type WebsocketClient struct {
	Dial WebsocketDialFunc
}

// HTTPClient is a replaceable function for executing an HTTP request.
type HTTPClient struct {
	Do HTTPDoFunc
}
