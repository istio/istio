// Copyright 2017 Istio Authors
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

// An example implementation of a client.

package forwarder

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

type request struct {
	URL       string
	Header    http.Header
	RequestID int
	Message   string
	Timeout   time.Duration
}

type protocol interface {
	makeRequest(ctx context.Context, req *request) (string, error)
	Close() error
}

func newProtocol(cfg Config) (protocol, error) {
	var httpDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	var wsDialContext func(network, addr string) (net.Conn, error)
	if len(cfg.UDS) > 0 {
		httpDialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.UDS)
		}

		wsDialContext = func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", cfg.UDS)
		}
	}

	rawURL := cfg.Request.Url
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed parsing request URL %s: %v", cfg.Request.Url, err)
	}

	timeout := common.GetTimeout(cfg.Request)
	headers := common.GetHeaders(cfg.Request)

	switch scheme.Instance(u.Scheme) {
	case scheme.HTTP, scheme.HTTPS:
		return &httpProtocol{
			client: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
					DialContext: httpDialContext,
				},
				Timeout: timeout,
			},
			do: cfg.Dialer.HTTP,
		}, nil
	case scheme.GRPC, scheme.GRPCS:
		// grpc-go sets incorrect authority header
		authority := headers.Get(hostHeader)

		// transport security
		security := grpc.WithInsecure()
		if scheme.Instance(u.Scheme) == scheme.GRPCS {
			creds, err := credentials.NewClientTLSFromFile(cfg.TLSCert, authority)
			if err != nil {
				log.Fatalf("failed to load client certs %s %v", cfg.TLSCert, err)
			}
			security = grpc.WithTransportCredentials(creds)
		}

		// Strip off the scheme from the address.
		address := rawURL[len(u.Scheme+"://"):]

		// Connect to the GRPC server.
		ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
		defer cancel()
		grpcConn, err := cfg.Dialer.GRPC(ctx,
			address,
			security,
			grpc.WithAuthority(authority),
			grpc.WithBlock())
		if err != nil {
			return nil, err
		}
		return &grpcProtocol{
			conn:   grpcConn,
			client: proto.NewEchoTestServiceClient(grpcConn),
		}, nil
	case scheme.WebSocket, scheme.WebSocketS:
		dialer := &websocket.Dialer{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			NetDial:          wsDialContext,
			HandshakeTimeout: timeout,
		}
		return &websocketProtocol{
			dialer: dialer,
		}, nil
	}

	return nil, fmt.Errorf("unrecognized protocol %q", u.String())
}
