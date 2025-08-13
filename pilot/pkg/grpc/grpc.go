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
	"math"
	"strings"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/features"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/util/sets"
)

func ServerOptions(options *istiokeepalive.Options, statHandler func(int64), interceptors ...grpc.UnaryServerInterceptor) []grpc.ServerOption {
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
		grpc.StatsHandler(StatsHandler(statHandler)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

func StatsHandler(callback func(int64)) stats.Handler {
	return grpcStatsHandler{max: atomic.NewInt64(0), callback: callback}
}

type grpcStatsHandler struct {
	max      *atomic.Int64
	callback func(int64)
}

func (h grpcStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h grpcStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h grpcStatsHandler) HandleConn(ctx context.Context, s stats.ConnStats) {
}

func (h grpcStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	if ts, ok := s.(*stats.InPayload); ok {
		l := int64(ts.Length)
		// Set h.max = max(h.max, l)
		// We need a CAS loop here, there is no native support for t his.
		for {
			cur := h.max.Load()
			if l < cur {
				return
			}
			if h.max.CompareAndSwap(cur, l) {
				// Success, exit
				h.callback(l)
				return
			}
		}
	}
}

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
	defaultInitialConnWindowSize       = 1024 * 1024 // default gRPC InitialWindowSize
	defaultInitialWindowSize           = 1024 * 1024 // default gRPC ConnWindowSize
)

// ClientOptions returns consistent grpc dial options with custom dial options
func ClientOptions(options *istiokeepalive.Options, tlsOpts *TLSOptions) ([]grpc.DialOption, error) {
	if options == nil {
		options = istiokeepalive.DefaultOption()
	}
	keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    options.Time,
		Timeout: options.Timeout,
	})

	initialWindowSizeOption := grpc.WithInitialWindowSize(int32(defaultInitialWindowSize))
	initialConnWindowSizeOption := grpc.WithInitialConnWindowSize(int32(defaultInitialConnWindowSize))
	msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize))
	var tlsDialOpts grpc.DialOption
	var err error
	if tlsOpts != nil {
		tlsDialOpts, err = getTLSDialOption(tlsOpts)
		if err != nil {
			return nil, err
		}
	} else {
		tlsDialOpts = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	return []grpc.DialOption{keepaliveOption, initialWindowSizeOption, initialConnWindowSizeOption, msgSizeOption, tlsDialOpts}, nil
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

type ErrorType string

const (
	// This indicates all the errors except the expected errors or graceful termination.
	UnexpectedError ErrorType = "unexpectedError"
	// This indicates an expected error when things are operating normally.
	ExpectedError ErrorType = "expectedError"
	// This indicates an error which happen when the connection is gracefully terminated.
	// For example, the peer calls `SendClose()`.
	GracefulTermination ErrorType = "gracefulTermination"
)

// GRPCErrorType checks a gRPC error code and determines its ErrorType.
// This is basically capturing when the peer disconnects.
func GRPCErrorType(err error) ErrorType {
	if err == io.EOF {
		return GracefulTermination
	}

	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.Canceled || s.Code() == codes.DeadlineExceeded {
			return ExpectedError
		}
		if s.Code() == codes.Unavailable && containsExpectedMessage(s.Message()) {
			return ExpectedError
		}
	}
	// If this is not a gRPCStatus we should just error message.
	if strings.Contains(err.Error(), "stream terminated by RST_STREAM with error code: NO_ERROR") {
		return ExpectedError
	}
	if strings.Contains(err.Error(), "received prior goaway: code: NO_ERROR") {
		return ExpectedError
	}

	return UnexpectedError
}
