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

package xds

import (
	"context"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"istio.io/istio/pilot/pkg/features"
	istiokeepalive "istio.io/istio/pkg/keepalive"
)

type (
	SendHandler  func(errorChan chan error)
	ErrorHandler func(err error)
)

var timeout = features.XdsPushSendTimeout

// Send with timeout if specified. If timeout is zero, sends without timeout.
func Send(ctx context.Context, send SendHandler, errorh ErrorHandler) error {
	errChan := make(chan error, 1)

	go func() {
		send(errChan)
		close(errChan)
	}()

	if timeout.Nanoseconds() > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		select {
		case <-timeoutCtx.Done():
			if errorh != nil {
				errorh(timeoutCtx.Err())
			}
			return timeoutCtx.Err()
		case err := <-errChan:
			if errorh != nil {
				errorh(err)
			}
			return err
		}
	} else {
		err := <-errChan
		errorh(err)
		return err
	}
}

func GrpcServerOptions(options *istiokeepalive.Options, interceptors ...grpc.UnaryServerInterceptor) []grpc.ServerOption {
	maxStreams := features.MaxConcurrentStreams
	maxRecvMsgSize := features.MaxRecvMsgSize

	log.Infof("using max conn age of %v", options.MaxServerConnectionAge)
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
