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
	"fmt"
	"io"
	"sync"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/tracing"
)

// grpcServer holds the state for the gRPC API server.
type grpcServer struct {
	handlers Handler
	attrMgr  attribute.Manager
	tracer   tracing.Tracer
	gp       *pool.GoroutinePool

	// replaceable sendMsg so we can inject errors in tests
	sendMsg func(grpc.Stream, proto.Message) error
}

// NewGRPCServer creates a gRPC serving stack.
func NewGRPCServer(handlers Handler, tracer tracing.Tracer, gp *pool.GoroutinePool) mixerpb.MixerServer {
	return &grpcServer{
		handlers: handlers,
		attrMgr:  attribute.NewManager(),
		tracer:   tracer,
		gp:       gp,
		sendMsg: func(stream grpc.Stream, m proto.Message) error {
			return stream.SendMsg(m)
		},
	}
}

// dispatcher does all the nitty-gritty details of handling the mixer's low-level API
// protocol and dispatching to the right API handler.
func (s *grpcServer) dispatcher(stream grpc.Stream, methodName string,
	getState func() (request proto.Message, response proto.Message, requestAttrs *mixerpb.Attributes, result *rpc.Status),
	worker func(context.Context, *attribute.MutableBag, proto.Message, proto.Message)) error {

	// tracks attribute state for this stream
	tracker := s.attrMgr.NewTracker()
	defer tracker.Done()

	// used to serialize sending on the grpc stream, since the grpc stream is not multithread-safe
	sendLock := &sync.Mutex{}

	root, ctx := s.tracer.StartRootSpan(stream.Context(), methodName)
	defer root.Finish()

	// ensure pending stuff is done before leaving
	wg := sync.WaitGroup{}
	defer wg.Wait()

	for {
		request, response, attrs, result := getState()

		// get a single message
		err := stream.RecvMsg(request)
		if err == io.EOF {
			return nil
		} else if err != nil {
			glog.Errorf("Stream error %s", err)
			return err
		}

		bag, err := tracker.ApplyAttributes(attrs)
		if err != nil {
			msg := fmt.Sprintf("Unable to process attribute update: %v", err)
			glog.Error(msg)
			*result = status.WithInvalidArgument(msg)

			sendLock.Lock()
			err = s.sendMsg(stream, response)
			sendLock.Unlock()

			if err != nil {
				glog.Errorf("Unable to send gRPC response message: %v", err)
			}

			continue
		}

		// throw the message into the work queue
		wg.Add(1)
		s.gp.ScheduleWork(func() {
			span, ctx2 := s.tracer.StartSpanFromContext(ctx, "RequestProcessing")
			span.LogFields(log.Object("gRPC request", request))

			// do the actual work for the message
			worker(ctx2, bag, request, response)

			sendLock.Lock()
			err := s.sendMsg(stream, response)
			sendLock.Unlock()

			if err != nil {
				glog.Errorf("Unable to send gRPC response message: %v", err)
			}

			bag.Done()

			span.LogFields(log.Object("gRPC response", response))
			span.Finish()

			wg.Done()
		})
	}
}

// Check is the entry point for the external Check method
func (s *grpcServer) Check(stream mixerpb.Mixer_CheckServer) error {
	return s.dispatcher(stream, "/istio.mixer.v1.Mixer/Check",
		func() (proto.Message, proto.Message, *mixerpb.Attributes, *rpc.Status) {
			request := &mixerpb.CheckRequest{}
			response := &mixerpb.CheckResponse{}
			return request, response, &request.AttributeUpdate, &response.Result
		},
		func(ctx context.Context, bag *attribute.MutableBag, request proto.Message, response proto.Message) {
			s.handlers.Check(ctx, bag, request.(*mixerpb.CheckRequest), response.(*mixerpb.CheckResponse))
		})
}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(stream mixerpb.Mixer_ReportServer) error {
	return s.dispatcher(stream, "/istio.mixer.v1.Mixer/Report",
		func() (proto.Message, proto.Message, *mixerpb.Attributes, *rpc.Status) {
			request := &mixerpb.ReportRequest{}
			response := &mixerpb.ReportResponse{}
			return request, response, &request.AttributeUpdate, &response.Result
		},
		func(ctx context.Context, bag *attribute.MutableBag, request proto.Message, response proto.Message) {
			s.handlers.Report(ctx, bag, request.(*mixerpb.ReportRequest), response.(*mixerpb.ReportResponse))
		})
}

// Quota is the entry point for the external Quota method
func (s *grpcServer) Quota(stream mixerpb.Mixer_QuotaServer) error {
	return s.dispatcher(stream, "/istio.mixer.v1.Mixer/Quota",
		func() (proto.Message, proto.Message, *mixerpb.Attributes, *rpc.Status) {
			request := &mixerpb.QuotaRequest{}
			response := &mixerpb.QuotaResponse{}
			return request, response, &request.AttributeUpdate, &response.Result
		},
		func(ctx context.Context, bag *attribute.MutableBag, request proto.Message, response proto.Message) {
			s.handlers.Quota(ctx, bag, request.(*mixerpb.QuotaRequest), response.(*mixerpb.QuotaResponse))
		})
}
