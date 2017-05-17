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
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/tracing"
)

type (
	// grpcServer holds the dispatchState for the gRPC API server.
	grpcServer struct {
		aspectDispatcher adapterManager.AspectDispatcher
		attrMgr          *attribute.Manager
		tracer           tracing.Tracer
		gp               *pool.GoroutinePool

		// replaceable sendMsg so we can inject errors in tests
		sendMsg func(grpc.Stream, proto.Message) error
	}

	// dispatchState holds the set of information used for dispatch and
	// request handling.
	dispatchState struct {
		request, response           proto.Message
		requestIndex, responseIndex *int64
		inAttrs, outAttrs           *mixerpb.Attributes
		result                      *rpc.Status
	}

	// dispatchArgs holds the set of information passed into dispatchFns.
	// These args are derived from the dispatchState.
	dispatchArgs struct {
		request, response       proto.Message
		requestIndex            int64
		requestBag, responseBag *attribute.MutableBag
		result                  *rpc.Status
	}

	stateGetterFn func() dispatchState
	dispatchFn    func(ctx context.Context, args dispatchArgs)
)

// NewGRPCServer creates a gRPC serving stack.
func NewGRPCServer(aspectDispatcher adapterManager.AspectDispatcher, tracer tracing.Tracer, gp *pool.GoroutinePool) mixerpb.MixerServer {
	return &grpcServer{
		aspectDispatcher: aspectDispatcher,
		attrMgr:          attribute.NewManager(),
		tracer:           tracer,
		gp:               gp,
		sendMsg: func(stream grpc.Stream, m proto.Message) error {
			return stream.SendMsg(m)
		},
	}
}

// dispatch does all the nitty-gritty details of handling Mixer's low-level API
// protocol and dispatching to the right API dispatchWrapperFn.
func (s *grpcServer) dispatch(stream grpc.Stream, methodName string, getState stateGetterFn, worker dispatchFn) error {

	// tracks attribute state for this stream
	reqTracker := s.attrMgr.NewTracker()
	respTracker := s.attrMgr.NewTracker()
	defer reqTracker.Done()
	defer respTracker.Done()

	// used to serialize sending on the grpc stream, since the grpc stream is not multithread-safe
	sendLock := &sync.Mutex{}

	root, ctx := s.tracer.StartRootSpan(stream.Context(), methodName)
	defer root.Finish()

	// ensure pending stuff is done before leaving
	wg := sync.WaitGroup{}
	defer wg.Wait()

	for {
		dState := getState()

		// get a single message
		err := stream.RecvMsg(dState.request)
		if err == io.EOF {
			return nil
		} else if err != nil {
			glog.Errorf("Reading from gRPC request stream failed: %v", err)
			return err
		}
		// ensure the indices are matched for processing
		*dState.responseIndex = *dState.requestIndex

		requestBag, err := reqTracker.ApplyProto(dState.inAttrs)
		if err != nil {
			msg := "Request could not be processed due to invalid 'attribute_update'."
			glog.Error(msg, "\n", err)
			details := status.NewBadRequest("attribute_update", err)
			*dState.result = status.InvalidWithDetails(msg, details)

			sendLock.Lock()
			err = s.sendMsg(stream, dState.response)
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
			span.LogFields(log.Object("gRPC request", dState.request))

			responseBag := attribute.GetMutableBag(nil)

			// do the actual work for the message
			args := dispatchArgs{
				dState.request,
				dState.response,
				*dState.requestIndex,
				requestBag,
				responseBag,
				dState.result,
			}

			worker(ctx2, args)

			sendLock.Lock()
			if err = respTracker.ApplyBag(responseBag, 0, dState.outAttrs); err != nil {
				*dState.outAttrs = mixerpb.Attributes{}
				glog.Errorf("Unable to apply response attribute bag, sending blank attributes: %v", err)
			}
			err = s.sendMsg(stream, dState.response)
			sendLock.Unlock()

			if err != nil {
				glog.Errorf("Unable to send gRPC response message: %v", err)
			}

			requestBag.Done()
			responseBag.Done()

			span.LogFields(log.Object("gRPC response", dState.response))
			span.Finish()

			wg.Done()
		})
	}
}

// Check is the entry point for the external Check method
func (s *grpcServer) Check(stream mixerpb.Mixer_CheckServer) error {
	return s.dispatch(stream, "/istio.mixer.v1.Mixer/Check", getCheckState, s.preprocess(s.handleCheck))
}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(stream mixerpb.Mixer_ReportServer) error {
	return s.dispatch(stream, "/istio.mixer.v1.Mixer/Report", getReportState, s.preprocess(s.handleReport))
}

// Quota is the entry point for the external Quota method
func (s *grpcServer) Quota(stream mixerpb.Mixer_QuotaServer) error {
	return s.dispatch(stream, "/istio.mixer.v1.Mixer/Quota", getQuotaState, s.preprocess(s.handleQuota))
}

func (s *grpcServer) handleCheck(ctx context.Context, args dispatchArgs) {

	resp := args.response.(*mixerpb.CheckResponse)

	if glog.V(2) {
		glog.Infof("Check [%x]", args.requestIndex)
	}

	*args.result = s.aspectDispatcher.Check(ctx, args.requestBag, args.responseBag)
	// TODO: this value needs to initially come from config, and be modulated by the kind of attribute
	//       that was used in the check and the in-used aspects (for example, maybe an auth check has a
	//       30s TTL but a whitelist check has got a 120s TTL)
	resp.Expiration = 5 * time.Second

	if glog.V(2) {
		glog.Infof("Check [%x] <-- %s", args.requestIndex, args.response)
	}
}

func (s *grpcServer) handleReport(ctx context.Context, args dispatchArgs) {
	if glog.V(2) {
		glog.Infof("Report [%x]", args.requestIndex)
	}

	*args.result = s.aspectDispatcher.Report(ctx, args.requestBag, args.responseBag)

	if glog.V(2) {
		glog.Infof("Report [%x] <-- %s", args.requestIndex, args.response)
	}
}

func (s *grpcServer) handleQuota(ctx context.Context, args dispatchArgs) {
	req := args.request.(*mixerpb.QuotaRequest)
	resp := args.response.(*mixerpb.QuotaResponse)

	if glog.V(2) {
		glog.Infof("Quota [%x]", req.RequestIndex)
	}

	qma := &aspect.QuotaMethodArgs{
		Quota:           req.Quota,
		Amount:          req.Amount,
		DeduplicationID: req.DeduplicationId,
		BestEffort:      req.BestEffort,
	}
	var qmr *aspect.QuotaMethodResp

	qmr, *args.result = s.aspectDispatcher.Quota(ctx, args.requestBag, args.responseBag, qma)

	if qmr != nil {
		resp.Amount = qmr.Amount
		resp.Expiration = qmr.Expiration
	}

	if glog.V(2) {
		glog.Infof("Quota [%x] <-- %s", req.RequestIndex, args.response)
	}
}

func (s *grpcServer) preprocess(dispatch dispatchFn) dispatchFn {
	return func(ctx context.Context, args dispatchArgs) {

		if glog.V(2) {
			glog.Infof("Preprocessing [%x]", args.requestIndex)
		}

		*args.result = s.aspectDispatcher.Preprocess(ctx, args.requestBag, args.responseBag)

		if glog.V(2) {
			glog.Infof("Preprocessing [%x] <-- %s", args.requestIndex, args.result)
		}

		if !status.IsOK(*args.result) {
			return
		}

		if err := args.requestBag.Merge(args.responseBag); err != nil {
			// TODO: better error messages that push internal details into debuginfo messages
			glog.Errorf("Could not merge mutable bags for request: %v", err)
			*args.result = status.WithInternal("The results from the request preprocessing could not be merged.")
			return
		}

		dispatch(ctx, args)
	}
}

func getCheckState() dispatchState {
	request := &mixerpb.CheckRequest{}
	response := &mixerpb.CheckResponse{}
	response.AttributeUpdate = &mixerpb.Attributes{}
	return dispatchState{
		request, response,
		&request.RequestIndex, &response.RequestIndex,
		&request.AttributeUpdate, response.AttributeUpdate,
		&response.Result,
	}
}

func getReportState() dispatchState {
	request := &mixerpb.ReportRequest{}
	response := &mixerpb.ReportResponse{}
	response.AttributeUpdate = &mixerpb.Attributes{}
	return dispatchState{
		request, response,
		&request.RequestIndex, &response.RequestIndex,
		&request.AttributeUpdate, response.AttributeUpdate,
		&response.Result,
	}
}

func getQuotaState() dispatchState {
	request := &mixerpb.QuotaRequest{}
	response := &mixerpb.QuotaResponse{}
	response.AttributeUpdate = &mixerpb.Attributes{}
	return dispatchState{
		request, response,
		&request.RequestIndex, &response.RequestIndex,
		&request.AttributeUpdate, response.AttributeUpdate,
		&response.Result,
	}
}
