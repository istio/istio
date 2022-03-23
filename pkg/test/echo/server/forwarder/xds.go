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
)

var _ protocol = &grpcProtocol{}

type xdsProtocol struct {
	conn *grpc.ClientConn
}

func newXDSProtocol(r *Config) (protocol, error) {
	var opts []grpc.DialOption
	// grpc-go sets incorrect authority header

	// transport security
	creds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		return nil, err
	}
	security := grpc.WithTransportCredentials(creds)
	if len(r.XDSTestBootstrap) > 0 {
		r, err := xdsresolver.NewXDSResolverWithConfigForTesting(r.XDSTestBootstrap)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithResolvers(r))
	}

	if r.getClientCertificate != nil {
		security = grpc.WithTransportCredentials(credentials.NewTLS(r.tlsConfig))
	}

	address := r.Request.Url

	// Connect to the GRPC server.
	ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
	defer cancel()
	opts = append(opts, security, grpc.WithAuthority(r.headers.Get(hostHeader)))
	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, err
	}

	return &xdsProtocol{
		conn: conn,
	}, nil
}

func (c *xdsProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
	return makeGRPCRequest(ctx, c.conn, req)
}

func (c *xdsProtocol) Close() error {
	return c.conn.Close()
}
