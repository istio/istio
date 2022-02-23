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

package echo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ io.Closer = &Client{}

// Client of an Echo server that simplifies request/response processing for Forward commands.
type Client struct {
	conn   *grpc.ClientConn
	client proto.EchoTestServiceClient
}

// New creates a new echo client.Instance that is connected to the given server address.
func New(address string, tlsSettings *common.TLSSettings, extraDialOpts ...grpc.DialOption) (*Client, error) {
	// Connect to the GRPC (command) endpoint of 'this' app.
	// TODO: make use of common.ConnectionTimeout once it increases
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dialOptions := []grpc.DialOption{grpc.WithBlock()}
	if tlsSettings != nil {
		cert, err := tls.X509KeyPair([]byte(tlsSettings.ClientCert), []byte(tlsSettings.Key))
		if err != nil {
			return nil, err
		}

		var certPool *x509.CertPool
		certPool, err = x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Cert from SystemCertPool: %v", err)
		}

		if tlsSettings.RootCert != "" && !certPool.AppendCertsFromPEM([]byte(tlsSettings.RootCert)) {
			return nil, fmt.Errorf("failed to create cert pool")
		}
		cfg := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: certPool})
		// If provided, override the hostname
		if tlsSettings.Hostname != "" {
			dialOptions = append(dialOptions, grpc.WithAuthority(tlsSettings.Hostname))
		}
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(cfg))
	} else if strings.HasPrefix(address, "xds:///") {
		creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
		if err != nil {
			return nil, err
		}
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(creds))
	} else {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	dialOptions = append(dialOptions, extraDialOpts...)
	conn, err := grpc.DialContext(ctx, address, dialOptions...)
	if err != nil {
		return nil, err
	}
	client := proto.NewEchoTestServiceClient(conn)

	return &Client{
		conn:   conn,
		client: client,
	}, nil
}

// Close the EchoClient and free any resources.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Echo(ctx context.Context, request *proto.EchoRequest) (Response, error) {
	resp, err := c.client.Echo(ctx, request)
	if err != nil {
		return Response{}, err
	}
	return parseResponse(resp.Message), nil
}

// ForwardEcho sends the given forward request and parses the response for easier processing. Only fails if the request fails.
func (c *Client) ForwardEcho(ctx context.Context, request *proto.ForwardEchoRequest) (Responses, error) {
	// Forward a request from 'this' service to the destination service.
	GlobalEchoRequests.Add(uint64(request.Count))
	resp, err := c.client.ForwardEcho(ctx, request)
	if err != nil {
		return nil, err
	}

	return ParseResponses(request, resp), nil
}

// GlobalEchoRequests records how many echo calls we have made total, from all sources.
// Note: go tests are distinct binaries per test suite, so this is the suite level number of calls
var GlobalEchoRequests = atomic.NewUint64(0)
