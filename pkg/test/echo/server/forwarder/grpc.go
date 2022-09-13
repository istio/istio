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

package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &grpcProtocol{}

type grpcProtocol struct {
	e *executor
}

func newGRPCProtocol(e *executor) protocol {
	return &grpcProtocol{e: e}
}

type grpcConnectionGetter func() (*grpc.ClientConn, func(), error)

func (c *grpcProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	var getConn grpcConnectionGetter
	if cfg.newConnectionPerRequest {
		// Create a new connection per request.
		getConn = func() (*grpc.ClientConn, func(), error) {
			conn, err := newGRPCConnection(cfg)
			if err != nil {
				return nil, nil, err
			}
			return conn, func() { _ = conn.Close() }, nil
		}
	} else {
		// Reuse the connection across all requests.
		conn, err := newGRPCConnection(cfg)
		if err != nil {
			return nil, err
		}
		defer func() { _ = conn.Close() }()
		getConn = func() (*grpc.ClientConn, func(), error) {
			return conn, func() {}, nil
		}
	}

	call := &grpcCall{
		e:       c.e,
		getConn: getConn,
	}
	return doForward(ctx, cfg, c.e, call.makeRequest)
}

func (c *grpcProtocol) Close() error {
	return nil
}

type grpcCall struct {
	e       *executor
	getConn grpcConnectionGetter
}

func (c *grpcCall) makeRequest(ctx context.Context, cfg *Config, requestID int) (string, error) {
	conn, closeConn, err := c.getConn()
	if err != nil {
		return "", err
	}
	defer closeConn()

	// Set the per-request timeout.
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	// Add headers to the request context.
	outMD := make(metadata.MD)
	for k, v := range cfg.headers {
		// Exclude the Host header from the GRPC context.
		if !strings.EqualFold(hostHeader, k) {
			outMD.Set(k, v...)
		}
	}
	outMD.Set("X-Request-Id", strconv.Itoa(requestID))
	ctx = metadata.NewOutgoingContext(ctx, outMD)

	var outBuffer bytes.Buffer
	grpcReq := &proto.EchoRequest{
		Message: cfg.Request.Message,
	}
	// TODO(nmittler): This doesn't fit in with the field pattern. Do we need this?
	outBuffer.WriteString(fmt.Sprintf("[%d] grpcecho.Echo(%v)\n", requestID, cfg.Request))

	start := time.Now()
	client := proto.NewEchoTestServiceClient(conn)
	resp, err := client.Echo(ctx, grpcReq)
	if err != nil {
		return "", err
	}

	echo.LatencyField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%v", time.Since(start)))
	echo.ActiveRequestsField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%d", c.e.ActiveRequests()))

	// When the underlying HTTP2 request returns status 404, GRPC
	// request does not return an error in grpc-go.
	// Instead, it just returns an empty response
	for _, line := range strings.Split(resp.GetMessage(), "\n") {
		if line != "" {
			echo.WriteBodyLine(&outBuffer, requestID, line)
		}
	}
	return outBuffer.String(), nil
}

func newGRPCConnection(cfg *Config) (*grpc.ClientConn, error) {
	var security grpc.DialOption
	if cfg.secure {
		security = grpc.WithTransportCredentials(credentials.NewTLS(cfg.tlsConfig))
	} else {
		security = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	opts := []grpc.DialOption{
		grpc.WithAuthority(cfg.hostHeader),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return newDialer(cfg).DialContext(ctx, "tcp", addr)
		}),
		security,
	}

	// Strip off the scheme from the address (for regular gRPC).
	address := cfg.Request.Url[len(cfg.scheme+"://"):]

	// Connect to the GRPC server.
	ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
	defer cancel()
	return grpc.DialContext(ctx, address, opts...)
}
