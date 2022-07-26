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

package xdstest

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"istio.io/pkg/log"
)

func safeSleep(ctx context.Context, t time.Duration) {
	select {
	case <-time.After(t):
	case <-ctx.Done():
	}
}

type slowClientStream struct {
	grpc.ClientStream
	recv, send time.Duration
}

func (w *slowClientStream) RecvMsg(m any) error {
	if w.recv > 0 {
		safeSleep(w.Context(), w.recv)
		log.Infof("delayed recv for %v", w.recv)
	}
	return w.ClientStream.RecvMsg(m)
}

func (w *slowClientStream) SendMsg(m any) error {
	if w.send > 0 {
		safeSleep(w.Context(), w.send)
		log.Infof("delayed send for %v", w.send)
	}
	return w.ClientStream.SendMsg(m)
}

// SlowClientInterceptor is an interceptor that allows injecting delays on Send and Recv
func SlowClientInterceptor(recv, send time.Duration) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		return &slowClientStream{clientStream, recv, send}, err
	}
}

type slowServerStream struct {
	grpc.ServerStream
	recv, send time.Duration
}

func (w *slowServerStream) RecvMsg(m any) error {
	if w.recv > 0 {
		safeSleep(w.Context(), w.recv)
		log.Infof("delayed recv for %v", w.recv)
	}
	return w.ServerStream.RecvMsg(m)
}

func (w *slowServerStream) SendMsg(m any) error {
	if w.send > 0 {
		safeSleep(w.Context(), w.send)
		log.Infof("delayed send for %v", w.send)
	}
	return w.ServerStream.SendMsg(m)
}

// SlowServerInterceptor is an interceptor that allows injecting delays on Send and Recv
func SlowServerInterceptor(recv, send time.Duration) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &slowServerStream{ss, recv, send})
	}
}
