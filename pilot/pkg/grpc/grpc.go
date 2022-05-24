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

package grpc

import (
	"context"
	"io"
	"strings"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/features"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/util/sets"
)

type SendHandler func() error

// Send with timeout if specified. If timeout is zero, sends without timeout.
func Send(ctx context.Context, send SendHandler) error {
	if features.XdsPushSendTimeout.Nanoseconds() > 0 {
		errChan := make(chan error, 1)
		timeoutCtx, cancel := context.WithTimeout(ctx, features.XdsPushSendTimeout)
		defer cancel()
		go func() {
			err := send()
			errChan <- err
			close(errChan)
		}()
		select {
		case <-timeoutCtx.Done():
			return status.Errorf(codes.DeadlineExceeded, "timeout sending")
		case err := <-errChan:
			return err
		}
	}
	err := send()
	return err
}

func ServerOptions(options *istiokeepalive.Options, interceptors ...grpc.UnaryServerInterceptor) []grpc.ServerOption {
	maxStreams := features.MaxConcurrentStreams
	maxRecvMsgSize := features.MaxRecvMsgSize

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		// Ensure we allow clients sufficient ability to send keep alives. If this is higher than client
		// keep alive setting, it will prematurely get a GOAWAY sent.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: options.Time / 2,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

var expectedGrpcFailureMessages = sets.New(
	"client disconnected",
	"error reading from server: EOF",
	"transport is closing",
)

func containsExpectedMessage(msg string) bool {
	for m := range expectedGrpcFailureMessages {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// IsExpectedGRPCError checks a gRPC error code and determines whether it is an expected error when
// things are operating normally. This is basically capturing when the client disconnects.
func IsExpectedGRPCError(err error) bool {
	if err == io.EOF {
		return true
	}

	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.Canceled || s.Code() == codes.DeadlineExceeded {
			return true
		}
		if s.Code() == codes.Unavailable && containsExpectedMessage(s.Message()) {
			return true
		}
	}
	// If this is not a gRPCStatus we should just error message.
	if strings.Contains(err.Error(), "stream terminated by RST_STREAM with error code: NO_ERROR") {
		return true
	}

	return false
}
