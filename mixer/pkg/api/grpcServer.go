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

	"github.com/gogo/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"
	opentracing "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc/codes"
	grpc "google.golang.org/grpc/status"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/checkcache"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime/dispatcher"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/pkg/log"
)

type (
	// grpcServer holds the dispatchState for the gRPC API server.
	grpcServer struct {
		dispatcher dispatcher.Dispatcher
		gp         *pool.GoroutinePool
		cache      *checkcache.Cache

		// the global dictionary. This will eventually be writable via config
		globalWordList []string
		globalDict     map[string]int32
	}
)

var lg = log.RegisterScope("api", "API dispatcher messages.", 0)

// NewGRPCServer creates a gRPC serving stack.
func NewGRPCServer(dispatcher dispatcher.Dispatcher, gp *pool.GoroutinePool, cache *checkcache.Cache) mixerpb.MixerServer {
	list := attribute.GlobalList()
	globalDict := make(map[string]int32, len(list))
	for i := 0; i < len(list); i++ {
		globalDict[list[i]] = int32(i)
	}

	return &grpcServer{
		dispatcher:     dispatcher,
		gp:             gp,
		globalWordList: list,
		globalDict:     globalDict,
		cache:          cache,
	}
}

// Check is the entry point for the external Check method
func (s *grpcServer) Check(ctx context.Context, req *mixerpb.CheckRequest) (*mixerpb.CheckResponse, error) {
	lg.Debugf("Check (GlobalWordCount:%d, DeduplicationID:%s, Quota:%v)", req.GlobalWordCount, req.DeduplicationId, req.Quotas)
	lg.Debug("Dispatching Preprocess Check")

	// bag around the input proto that keeps track of reference attributes
	protoBag := attribute.NewProtoBag(&req.Attributes, s.globalDict, s.globalWordList)

	if s.cache != nil {
		if value, ok := s.cache.Get(protoBag); ok {
			resp := &mixerpb.CheckResponse{
				Precondition: mixerpb.CheckResponse_PreconditionResult{
					Status: rpc.Status{
						Code:    value.StatusCode,
						Message: value.StatusMessage,
					},
					ValidDuration:        value.Expiration.Sub(time.Now()),
					ValidUseCount:        value.ValidUseCount,
					ReferencedAttributes: &value.ReferencedAttributes,
					RouteDirective:       value.RouteDirective,
				},
			}

			if status.IsOK(resp.Precondition.Status) {
				log.Debug("Check approved from cache")
			} else {
				log.Debugf("Check denied from cache: %v", resp.Precondition.Status)
			}

			if !status.IsOK(resp.Precondition.Status) || len(req.Quotas) == 0 {
				// we found a cached result and no quotas to allocate, so we're outta here
				return resp, nil
			}
		}
	}

	// This holds the output state of preprocess operations
	checkBag := attribute.GetMutableBag(protoBag)

	resp, err := s.check(ctx, req, protoBag, checkBag)

	protoBag.Done()
	checkBag.Done()

	return resp, err
}

func (s *grpcServer) check(ctx context.Context, req *mixerpb.CheckRequest,
	protoBag *attribute.ProtoBag, checkBag *attribute.MutableBag) (*mixerpb.CheckResponse, error) {

	globalWordCount := int(req.GlobalWordCount)

	if err := s.dispatcher.Preprocess(ctx, protoBag, checkBag); err != nil {
		err = fmt.Errorf("preprocessing attributes failed: %v", err)
		lg.Errora("Check failed:", err.Error())
		return nil, grpc.Errorf(codes.Internal, err.Error())
	}

	lg.Debug("Dispatching to main adapters after running processors")
	lg.Debuga("Attribute Bag: \n", checkBag)
	lg.Debug("Dispatching Check")

	// snapshot the state after we've called the APAs so that we can reuse it
	// for every check + quota call.
	snapApa := protoBag.SnapshotReferencedAttributes()

	cr, dir, err := s.dispatcher.Check(ctx, checkBag)
	if err != nil {
		err = fmt.Errorf("performing check operation failed: %v", err)
		lg.Errora("Check failed:", err.Error())
		return nil, grpc.Errorf(codes.Internal, err.Error())
	}

	if status.IsOK(cr.Status) {
		lg.Debug("Check approved")
	} else {
		lg.Debugf("Check denied: %v", cr.Status)
	}

	resp := &mixerpb.CheckResponse{
		Precondition: mixerpb.CheckResponse_PreconditionResult{
			ValidDuration:        cr.ValidDuration,
			ValidUseCount:        cr.ValidUseCount,
			Status:               cr.Status,
			ReferencedAttributes: protoBag.GetReferencedAttributes(s.globalDict, globalWordCount),
			RouteDirective:       dir,
		},
	}

	if s.cache != nil {
		// keep this for later...
		s.cache.Set(protoBag, checkcache.Value{
			StatusCode:           resp.Precondition.Status.Code,
			StatusMessage:        resp.Precondition.Status.Message,
			Expiration:           time.Now().Add(resp.Precondition.ValidDuration),
			ValidUseCount:        resp.Precondition.ValidUseCount,
			ReferencedAttributes: *resp.Precondition.ReferencedAttributes,
			RouteDirective:       resp.Precondition.RouteDirective,
		})
	}

	if status.IsOK(resp.Precondition.Status) && len(req.Quotas) > 0 {
		resp.Quotas = make(map[string]mixerpb.CheckResponse_QuotaResult, len(req.Quotas))

		for name, param := range req.Quotas {
			qma := dispatcher.QuotaMethodArgs{
				Quota:           name,
				Amount:          param.Amount,
				DeduplicationID: req.DeduplicationId + name,
				BestEffort:      param.BestEffort,
			}

			// restore to the post-APA state
			protoBag.RestoreReferencedAttributes(snapApa)

			lg.Debuga("Dispatching Quota: ", qma.Quota)

			crqr := mixerpb.CheckResponse_QuotaResult{}

			qr, err := s.dispatcher.Quota(ctx, checkBag, qma)
			if err != nil {
				err = fmt.Errorf("performing quota alloc failed: %v", err)
				lg.Errora("Quota failure:", err.Error())
				// we continue the quota loop even after this error
			} else {
				if !status.IsOK(qr.Status) {
					lg.Debugf("Quota denied: %v", qr.Status)
				}
				crqr.ValidDuration = qr.ValidDuration
				crqr.GrantedAmount = qr.Amount
			}

			lg.Debugf("Quota '%s' result: %#v", qma.Quota, crqr)

			crqr.ReferencedAttributes = *protoBag.GetReferencedAttributes(s.globalDict, globalWordCount)
			resp.Quotas[name] = crqr
		}
	}

	return resp, nil
}

var reportResp = &mixerpb.ReportResponse{}

// Report is the entry point for the external Report method
func (s *grpcServer) Report(ctx context.Context, req *mixerpb.ReportRequest) (*mixerpb.ReportResponse, error) {
	lg.Debugf("Report (Count: %d)", len(req.Attributes))

	if len(req.Attributes) == 0 {
		// early out
		return reportResp, nil
	}

	// apply the request-level word list to each attribute message if needed
	for i := 0; i < len(req.Attributes); i++ {
		if len(req.Attributes[i].Words) == 0 {
			req.Attributes[i].Words = req.DefaultWords
		}
	}

	// bag around the input proto that keeps track of reference attributes
	protoBag := attribute.NewProtoBag(&req.Attributes[0], s.globalDict, s.globalWordList)

	// This tracks the delta attributes encoded in the individual report entries
	accumBag := attribute.GetMutableBag(protoBag)

	// This holds the output state of preprocess operations, which ends up as a delta over the current accumBag.
	reportBag := attribute.GetMutableBag(accumBag)

	reportSpan, reportCtx := opentracing.StartSpanFromContext(ctx, "Report")
	reporter := s.dispatcher.GetReporter(reportCtx)

	var errors *multierror.Error
	for i := 0; i < len(req.Attributes); i++ {
		span, newctx := opentracing.StartSpanFromContext(reportCtx, fmt.Sprintf("attribute bag %d", i))

		// the first attribute block is handled by the protoBag as a foundation,
		// deltas are applied to the child bag (i.e. requestBag)
		if i > 0 {
			if err := accumBag.UpdateBagFromProto(&req.Attributes[i], s.globalWordList); err != nil {
				err = fmt.Errorf("request could not be processed due to invalid attributes: %v", err)
				span.LogFields(otlog.String("error", err.Error()))
				span.Finish()
				errors = multierror.Append(errors, err)
				break
			}
		}

		lg.Debug("Dispatching Preprocess")

		if err := s.dispatcher.Preprocess(newctx, accumBag, reportBag); err != nil {
			err = fmt.Errorf("preprocessing attributes failed: %v", err)
			span.LogFields(otlog.String("error", err.Error()))
			span.Finish()
			errors = multierror.Append(errors, err)
			continue
		}

		lg.Debug("Dispatching to main adapters after running preprocessors")
		lg.Debuga("Attribute Bag: \n", reportBag)
		lg.Debugf("Dispatching Report %d out of %d", i+1, len(req.Attributes))

		if err := reporter.Report(reportBag); err != nil {
			span.LogFields(otlog.String("error", err.Error()))
			span.Finish()
			errors = multierror.Append(errors, err)
			continue
		}

		span.Finish()

		// purge the effect of the Preprocess call so that the next time through everything is clean
		reportBag.Reset()
	}

	reportBag.Done()
	accumBag.Done()
	protoBag.Done()

	if err := reporter.Flush(); err != nil {
		errors = multierror.Append(errors, err)
	}
	reporter.Done()

	if errors != nil {
		reportSpan.LogFields(otlog.String("error", errors.Error()))
	}
	reportSpan.Finish()

	if errors != nil {
		lg.Errora("Report failed:", errors.Error())
		return nil, grpc.Errorf(codes.Unknown, errors.Error())
	}

	return reportResp, nil
}
