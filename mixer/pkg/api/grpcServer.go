// Copyright 2016 Istio Authors
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

package api

// gRPC server. The GRPCServer type handles incoming streaming gRPC traffic and invokes method-specific
// handlers to implement the method-specific logic.
//
// When you create a GRPCServer instance, you specify a number of transport-level options, along with the
// set of method handlers responsible for the logic of individual API methods

// TODO: Once the gRPC code is updated to use context objects from "context" as
// opposed to from "golang.org/x/net/context", this code should be updated to
// pass the context from the gRPC streams to downstream calls as opposed to merely
// using context.Background.

import (
	"context"
	"io"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/tracing"
)

// grpcServer holds the state for the gRPC API server.
type grpcServer struct {
	handlers Handler
	attrMgr  attribute.Manager
	tracer   tracing.Tracer
}

// NewGRPCServer creates a gRPC serving stack.
func NewGRPCServer(handlers Handler, tracer tracing.Tracer) mixerpb.MixerServer {
	return &grpcServer{
		handlers: handlers,
		attrMgr:  attribute.NewManager(),
		tracer:   tracer,
	}
}

type handlerFunc func(ctx context.Context, tracker attribute.Tracker, request proto.Message, response proto.Message)

func (s *grpcServer) streamLoop(stream grpc.ServerStream, request proto.Message, response proto.Message,
	handler handlerFunc, methodName string) error {
	tracker := s.attrMgr.NewTracker()
	defer tracker.Done()

	root, ctx := s.tracer.StartRootSpan(stream.Context(), methodName)
	defer root.Finish()

	for {
		// get a single message
		if err := stream.RecvMsg(request); err == io.EOF {
			return nil
		} else if err != nil {
			glog.Errorf("Stream error %s", err)
			return err
		}

		var span ot.Span
		span, ctx = s.tracer.StartSpanFromContext(ctx, methodName)
		span.LogFields(log.Object("gRPC request", request))

		// do the actual work for the message
		// TODO: do we want to carry the same ctx through each loop iteration, or pull it out of the stream each time?
		handler(ctx, tracker, request, response)

		// produce the response
		if err := stream.SendMsg(response); err != nil {
			return err
		}

		span.LogFields(log.Object("gRPC response", response))
		span.Finish()

		// reset everything to 0
		request.Reset()
		response.Reset()
	}
}

// Check is the entry point for the external Check method
func (s *grpcServer) Check(stream mixerpb.Mixer_CheckServer) error {
	return s.streamLoop(stream,
		new(mixerpb.CheckRequest),
		new(mixerpb.CheckResponse),
		func(ctx context.Context, tracker attribute.Tracker, request proto.Message, response proto.Message) {
			s.handlers.Check(ctx, tracker, request.(*mixerpb.CheckRequest), response.(*mixerpb.CheckResponse))
		}, "/istio.mixer.v1.Mixer/Check")
}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(stream mixerpb.Mixer_ReportServer) error {
	return s.streamLoop(stream,
		new(mixerpb.ReportRequest),
		new(mixerpb.ReportResponse),
		func(ctx context.Context, tracker attribute.Tracker, request proto.Message, response proto.Message) {
			s.handlers.Report(ctx, tracker, request.(*mixerpb.ReportRequest), response.(*mixerpb.ReportResponse))
		}, "/istio.mixer.v1.Mixer/Report")
}

// Quota is the entry point for the external Quota method
func (s *grpcServer) Quota(stream mixerpb.Mixer_QuotaServer) error {
	return s.streamLoop(stream,
		new(mixerpb.QuotaRequest),
		new(mixerpb.QuotaResponse),
		func(ctx context.Context, tracker attribute.Tracker, request proto.Message, response proto.Message) {
			s.handlers.Quota(ctx, tracker, request.(*mixerpb.QuotaRequest), response.(*mixerpb.QuotaResponse))
		}, "/istio.mixer.v1.Mixer/Quota")
}
