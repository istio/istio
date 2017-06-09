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

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"
	legacyContext "golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

// We have a slightly messy situation around the use of context objects. gRPC stubs are
// generated to expect the old "x/net/context" types instead of the more modern "context".
// We end up doing a quick switcharoo from the gRPC defined type to the modern type so we can
// use the modern type elsewhere in the code.

type (
	// grpcServer holds the dispatchState for the gRPC API server.
	grpcServer struct {
		aspectDispatcher adapterManager.AspectDispatcher
		gp               *pool.GoroutinePool

		// the global dictionary. This will eventually be writable via config
		words   []string
		wordMap map[string]int32
	}

	// dispatchState holds the information used for dispatch and
	// request handling.
	dispatchState struct {
		inAttrs, outAttrs *mixerpb.Attributes
		result            *rpc.Status
		requestBag        *attribute.MutableBag // optional
	}

	dispatchFn func(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status
)

// NewGRPCServer creates a gRPC serving stack.
func NewGRPCServer(aspectDispatcher adapterManager.AspectDispatcher, gp *pool.GoroutinePool) mixerpb.MixerServer {
	words := globalWordList
	wordMap := make(map[string]int32, len(words))
	for i := 0; i < len(words); i++ {
		wordMap[words[i]] = int32(i)
	}

	return &grpcServer{
		aspectDispatcher: aspectDispatcher,
		gp:               gp,
		words:            words,
		wordMap:          wordMap,
	}
}

// dispatch implements all the nitty-gritty details of handling Mixer's low-level API
// protocol and dispatching to an appropriate API worker.
func (s *grpcServer) dispatch(ctx context.Context, dState *dispatchState, worker dispatchFn) error {
	requestBag := dState.requestBag
	err := requestBag.UpdateBagFromProto(dState.inAttrs, s.words)
	if err != nil {
		msg := "Request could not be processed due to invalid 'attributes'."
		glog.Error(msg, "\n", err)
		details := status.NewBadRequest("attributes", err)
		out := status.InvalidWithDetails(msg, details)
		return makeGRPCError(out)
	}

	// the preproc atttributes will be in a child bag
	preprocResponseBag := attribute.GetMutableBag(requestBag)

	out := s.aspectDispatcher.Preprocess(ctx, requestBag, preprocResponseBag)
	if status.IsOK(out) {
		responseBag := attribute.GetMutableBag(nil)

		if glog.V(2) {
			glog.Info("Dispatching to main adapters after running processors")
			for _, name := range preprocResponseBag.Names() {
				v, _ := preprocResponseBag.Get(name)
				glog.Infof("  %s: %v", name, v)
			}
		}

		// do the actual work for the message
		out = worker(ctx, preprocResponseBag, responseBag)

		if dState.result != nil {
			*dState.result = out
			out = status.OK
		}

		if dState.outAttrs != nil {
			responseBag.ToProto(dState.outAttrs, s.wordMap)
		}
		responseBag.Done()
	}

	preprocResponseBag.Done()
	return makeGRPCError(out)
}

func makeGRPCError(status rpc.Status) error {
	return grpc.Errorf(codes.Code(status.Code), status.Message)
}

// Check is the entry point for the external Check method
func (s *grpcServer) Check(legacyCtx legacyContext.Context, req *mixerpb.CheckRequest) (*mixerpb.CheckResponse, error) {
	resp := &mixerpb.CheckResponse{}

	dState := dispatchState{
		inAttrs:    &req.Attributes,
		outAttrs:   &resp.Attributes,
		result:     &resp.Status,
		requestBag: attribute.GetMutableBag(nil),
	}

	err := s.dispatch(legacyCtx, &dState, func(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status {
		glog.Info("Dispatching Check")
		out := s.aspectDispatcher.Check(ctx, requestBag, responseBag)
		glog.Info("Check returned with: ", statusString(out))

		// TODO: these values need to initially come from config, and be modulated by the kind of attribute
		//       that was used in the check and the in-used aspects (for example, maybe an auth check has a
		//       30s TTL but a whitelist check has got a 120s TTL)
		resp.Cachability.Duration = 5 * time.Second
		resp.Cachability.UseCount = 10000

		return out
	})

	dState.requestBag.Done()
	return resp, err
}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(legacyCtx legacyContext.Context, req *mixerpb.ReportRequest) (*mixerpb.ReportResponse, error) {
	resp := &mixerpb.ReportResponse{}

	requestBag := attribute.GetMutableBag(nil)
	for i := 0; i < len(req.Attributes); i++ {
		if len(req.Attributes[i].Words) == 0 {
			req.Attributes[i].Words = req.DefaultWords
		}

		dState := dispatchState{
			inAttrs:    &req.Attributes[i],
			outAttrs:   nil,
			result:     nil,
			requestBag: requestBag,
		}

		err := s.dispatch(legacyCtx, &dState, func(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status {
			glog.Info("Dispatching Report")
			out := s.aspectDispatcher.Report(ctx, requestBag, responseBag)
			glog.Info("Report returned with: ", statusString(out))
			return out
		})

		if err != nil {
			requestBag.Done()
			return resp, err
		}
	}

	requestBag.Done()
	return resp, nil
}

// Quota is the entry point for the external Quota method
func (s *grpcServer) Quota(legacyCtx legacyContext.Context, req *mixerpb.QuotaRequest) (*mixerpb.QuotaResponse, error) {
	resp := &mixerpb.QuotaResponse{}

	dState := dispatchState{
		inAttrs:    &req.Attributes,
		outAttrs:   nil,
		result:     nil,
		requestBag: attribute.GetMutableBag(nil),
	}

	err := s.dispatch(legacyCtx, &dState, func(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status {
		qma := &aspect.QuotaMethodArgs{
			Quota:           req.Quota,
			Amount:          req.Amount,
			DeduplicationID: req.DeduplicationId,
			BestEffort:      req.BestEffort,
		}

		glog.Info("Dispatching Quota")
		qmr, out := s.aspectDispatcher.Quota(ctx, requestBag, responseBag, qma)
		glog.Infof("Quota returned with status '%v' and quota response '%v'", statusString(out), qmr)

		if qmr != nil {
			resp.Amount = qmr.Amount
			resp.Expiration = qmr.Expiration
		}

		return out
	})

	dState.requestBag.Done()
	return resp, err
}

func statusString(status rpc.Status) string {
	if name, ok := rpc.Code_name[status.Code]; ok {
		return fmt.Sprintf("%s %s", name, status.Message)
	}
	return "Unknown " + status.Message
}
