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
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/xds"
	xdsresolver "google.golang.org/grpc/xds"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &grpcProtocol{}

type xdsProtocol struct {
	e *executor
}

func newXDSProtocol(e *executor) protocol {
	return &xdsProtocol{e: e}
}

func (c *xdsProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	var getConn grpcConnectionGetter
	if cfg.newConnectionPerRequest {
		// Create a new connection per request.
		getConn = func() (*grpc.ClientConn, func(), error) {
			conn, err := newXDSConnection(cfg)
			if err != nil {
				return nil, nil, err
			}
			return conn, func() { _ = conn.Close() }, nil
		}
	} else {
		// Reuse the connection across all requests.
		conn, err := newXDSConnection(cfg)
		if err != nil {
			return nil, err
		}
		defer func() { _ = conn.Close() }()
		getConn = func() (*grpc.ClientConn, func(), error) {
			return conn, func() {}, nil
		}
	}

	call := grpcCall{
		e:       c.e,
		getConn: getConn,
	}
	return doForward(ctx, cfg, c.e, call.makeRequest)
}

func (c *xdsProtocol) Close() error {
	return nil
}

func newXDSConnection(cfg *Config) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	// transport security
	creds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		return nil, err
	}
	security := grpc.WithTransportCredentials(creds)
	if len(cfg.XDSTestBootstrap) > 0 {
		r, err := xdsresolver.NewXDSResolverWithConfigForTesting(cfg.XDSTestBootstrap)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithResolvers(r))
	}

	if cfg.getClientCertificate != nil {
		security = grpc.WithTransportCredentials(credentials.NewTLS(cfg.tlsConfig))
	}

	address := cfg.Request.Url

	// Connect to the GRPC server.
	ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
	defer cancel()
	opts = append(opts, security, grpc.WithAuthority(cfg.hostHeader))
	return grpc.DialContext(ctx, address, opts...)
}
