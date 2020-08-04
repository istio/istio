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
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &grpcProtocol{}

type grpcProtocol struct {
	conn   *grpc.ClientConn
	client proto.EchoTestServiceClient
}

func (c *grpcProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
	// Set the per-request timeout.
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	// Add headers to the request context.
	outMD := make(metadata.MD)
	for k, v := range req.Header {
		// Exclude the Host header from the GRPC context.
		if !strings.EqualFold(hostHeader, k) {
			outMD.Set(k, v...)
		}
	}
	outMD.Set("X-Request-Id", strconv.Itoa(req.RequestID))
	ctx = metadata.NewOutgoingContext(ctx, outMD)

	var outBuffer bytes.Buffer
	grpcReq := &proto.EchoRequest{
		Message: req.Message,
	}
	outBuffer.WriteString(fmt.Sprintf("[%d] grpcecho.Echo(%v)\n", req.RequestID, req))

	resp, err := c.client.Echo(ctx, grpcReq)
	if err != nil {
		return "", err
	}

	// when the underlying HTTP2 request returns status 404, GRPC
	// request does not return an error in grpc-go.
	// instead it just returns an empty response
	for _, line := range strings.Split(resp.GetMessage(), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}
	return outBuffer.String(), nil
}

func (c *grpcProtocol) Close() error {
	return c.conn.Close()
}
