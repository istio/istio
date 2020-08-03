// Copyright Istio Authors
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
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/echo/proto"
)

type request struct {
	URL         string
	Header      http.Header
	RequestID   int
	Message     string
	Timeout     time.Duration
	ServerFirst bool
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
		proto := &httpProtocol{
			client: &http.Client{
				Transport: &http.Transport{
					// We are creating a Transport on each ForwardEcho request. Transport is what holds connections,
					// so this means every ForwardEcho request will create a new connection. Without setting an idle timeout,
					// we would never close these connections.
					IdleConnTimeout: time.Second,
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
					DialContext: httpDialContext,
				},
				Timeout: timeout,
			},
			do: cfg.Dialer.HTTP,
		}
		if cfg.Request.Http2 && scheme.Instance(u.Scheme) == scheme.HTTPS {
			proto.client.Transport = &http2.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return tls.Dial(network, addr, cfg)
				},
			}
		} else if cfg.Request.Http2 {
			proto.client.Transport = &http2.Transport{
				// Golang doesn't have first class support for h2c, so we provide some workarounds
				// See https://www.mailgun.com/blog/http-2-cleartext-h2c-client-example-go/
				// So http2.Transport doesn't complain the URL scheme isn't 'https'
				AllowHTTP: true,
				// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			}
		}
		return proto, nil
	case scheme.GRPC:
		// grpc-go sets incorrect authority header
		authority := headers.Get(hostHeader)

		// transport security
		security := grpc.WithInsecure()

		// Strip off the scheme from the address.
		address := rawURL[len(u.Scheme+"://"):]

		// Connect to the GRPC server.
		ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
		defer cancel()
		grpcConn, err := cfg.Dialer.GRPC(ctx,
			address,
			security,
			grpc.WithAuthority(authority))
		if err != nil {
			return nil, err
		}
		return &grpcProtocol{
			conn:   grpcConn,
			client: proto.NewEchoTestServiceClient(grpcConn),
		}, nil
	case scheme.WebSocket:
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
	case scheme.TCP:
		dialer := net.Dialer{
			Timeout: timeout,
		}
		ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
		defer cancel()

		address := rawURL[len(u.Scheme+"://"):]
		tcpConn, err := cfg.Dialer.TCP(dialer, ctx, address)
		if err != nil {
			return nil, err
		}
		return &tcpProtocol{
			conn: tcpConn,
		}, nil
	}

	return nil, fmt.Errorf("unrecognized protocol %q", u.String())
}
