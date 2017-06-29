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

// Check is the entry point for the external Check method
func (s *grpcServer) Check(legacyCtx legacyContext.Context, req *mixerpb.CheckRequest) (*mixerpb.CheckResponse, error) {
	requestBag := attribute.GetMutableBag(nil)

	// TODO: this code doesn't distinguish between RPC failures when communicating with adapters and
	//       their backend vs. semantic failures. For example, if the adapterDispatcher.Check
	//       method returns a bad status, is that because an adapter failed an RPC or because the
	//       request was denied? This will need to be addressed in the new adapter model. In the meantime,
	//       RPC failure is treated as a semantic denial.

	err := requestBag.UpdateBagFromProto(&req.Attributes, s.words)
	if err != nil {
		msg := "Request could not be processed due to invalid attributes."
		glog.Error(msg, "\n", err)
		details := status.NewBadRequest("attributes", err)
		requestBag.Done()
		return nil, makeGRPCError(status.InvalidWithDetails(msg, details))
	}

	glog.Info("Dispatching Preprocess")
	preprocResponseBag := attribute.GetMutableBag(requestBag)
	out := s.aspectDispatcher.Preprocess(legacyCtx, requestBag, preprocResponseBag)
	glog.Info("Preprocess returned with: ", statusString(out))

	if !status.IsOK(out) {
		requestBag.Done()
		preprocResponseBag.Done()
		return nil, makeGRPCError(out)
	}

	if glog.V(2) {
		glog.Info("Dispatching to main adapters after running processors")
		for _, name := range preprocResponseBag.Names() {
			v, _ := preprocResponseBag.Get(name)
			glog.Infof("  %s: %v", name, v)
		}
	}

	responseBag := attribute.GetMutableBag(nil)

	glog.Info("Dispatching Check")
	out = s.aspectDispatcher.Check(legacyCtx, preprocResponseBag, responseBag)
	glog.Info("Check returned with: ", statusString(out))

	resp := &mixerpb.CheckResponse{}
	// TODO: these values need to initially come from config, and be modulated by the kind of attribute
	//       that was used in the check and the in-used aspects (for example, maybe an auth check has a
	//       30s TTL but a whitelist check has got a 120s TTL)
	resp.Precondition.ValidDuration = 5 * time.Second
	resp.Precondition.ValidUseCount = 10000
	resp.Precondition.Status = out
	responseBag.ToProto(&resp.Precondition.Attributes, s.wordMap)
	responseBag.Done()

	if len(req.Quotas) > 0 {
		resp.Quotas = make(map[string]mixerpb.CheckResponse_QuotaResult, len(req.Quotas))

		// TODO: should dispatch this loop in parallel
		for name, param := range req.Quotas {
			qma := &aspect.QuotaMethodArgs{
				Quota:           name,
				Amount:          param.Amount,
				DeduplicationID: req.DeduplicationId + name,
				BestEffort:      param.BestEffort,
			}

			glog.Info("Dispatching Quota")
			qmr, out := s.aspectDispatcher.Quota(legacyCtx, preprocResponseBag, qma)
			glog.Infof("Quota returned with status '%v' and quota response '%v'", statusString(out), qmr)

			if qmr == nil {
				qmr = &aspect.QuotaMethodResp{}
			}

			qr := mixerpb.CheckResponse_QuotaResult{
				GrantedAmount: qmr.Amount,
				ValidDuration: qmr.Expiration,
			}
			resp.Quotas[name] = qr
		}
	}

	requestBag.Done()
	preprocResponseBag.Done()

	return resp, nil
}

var reportResp = &mixerpb.ReportResponse{}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(legacyCtx legacyContext.Context, req *mixerpb.ReportRequest) (*mixerpb.ReportResponse, error) {
	requestBag := attribute.GetMutableBag(nil)
	preprocResponseBag := attribute.GetMutableBag(requestBag)

	var err error
	for i := 0; i < len(req.Attributes); i++ {
		if len(req.Attributes[i].Words) == 0 {
			// if the entry doesn't have any words of its own, use the request's words
			req.Attributes[i].Words = req.DefaultWords
		}

		err = requestBag.UpdateBagFromProto(&req.Attributes[i], s.words)
		if err != nil {
			msg := "Request could not be processed due to invalid attributes."
			glog.Error(msg, "\n", err)
			details := status.NewBadRequest("attributes", err)
			err = makeGRPCError(status.InvalidWithDetails(msg, details))
			break
		}

		glog.Info("Dispatching Preprocess")
		out := s.aspectDispatcher.Preprocess(legacyCtx, requestBag, preprocResponseBag)
		glog.Info("Preprocess returned with: ", statusString(out))

		if !status.IsOK(out) {
			err = makeGRPCError(out)
			break
		}

		if glog.V(2) {
			glog.Info("Dispatching to main adapters after running processors")
			for _, name := range preprocResponseBag.Names() {
				v, _ := preprocResponseBag.Get(name)
				glog.Infof("  %s: %v", name, v)
			}
		}

		glog.Info("Dispatching Report")
		out = s.aspectDispatcher.Report(legacyCtx, preprocResponseBag)
		glog.Info("Report returned with: ", statusString(out))

		if !status.IsOK(out) {
			err = makeGRPCError(out)
			break
		}

		preprocResponseBag.Reset()
	}

	preprocResponseBag.Done()
	requestBag.Done()

	if err != nil {
		return nil, err
	}

	return reportResp, nil
}

func makeGRPCError(status rpc.Status) error {
	return grpc.Errorf(codes.Code(status.Code), status.Message)
}

func statusString(status rpc.Status) string {
	if name, ok := rpc.Code_name[status.Code]; ok {
		return fmt.Sprintf("%s %s", name, status.Message)
	}
	return "Unknown " + status.Message
}
