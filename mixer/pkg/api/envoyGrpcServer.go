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

package api

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc/codes"
	grpc "google.golang.org/grpc/status"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	authz "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/checkcache"
	"istio.io/istio/mixer/pkg/loadshedding"
	"istio.io/istio/mixer/pkg/runtime/dispatcher"
	"istio.io/istio/mixer/pkg/status"
	attr "istio.io/pkg/attribute"
	"istio.io/pkg/pool"
)

type (
	//This struct mirrors grpcServer in grpcServer.go.
	//We define it separately since both have a Check Function which overlaps.
	// GrpcServerEnvoy holds the dispatchState for the gRPC API server
	GrpcServerEnvoy struct {
		dispatcher dispatcher.Dispatcher
		gp         *pool.GoroutinePool
		cache      *checkcache.Cache

		// the global dictionary. This will eventually be writable via config
		globalWordList []string
		globalDict     map[string]int32

		// load shedding
		throttler *loadshedding.Throttler
	}
)

//This function creates a new GrpcServerEnvoy struct with the given parameters and returns a pointer to it
func NewGRPCServerEnvoy(dispatcher dispatcher.Dispatcher, gp *pool.GoroutinePool, cache *checkcache.Cache,
	throttler *loadshedding.Throttler) *GrpcServerEnvoy {
	list := attribute.GlobalList()
	globalDict := make(map[string]int32, len(list))
	for i := 0; i < len(list); i++ {
		globalDict[list[i]] = int32(i)
	}
	return &GrpcServerEnvoy{
		dispatcher:     dispatcher,
		gp:             gp,
		globalWordList: list,
		globalDict:     globalDict,
		cache:          cache,
		throttler:      throttler,
	}
}

// Mirrors Mixer Check but instead uses Envoy External Authorization API
// It enables longevity of OOP adapters
//https://www.envoyproxy.io/docs/envoy/latest/api-v2/config/filter/http/ext_authz/v2/ext_authz.proto
func (s *GrpcServerEnvoy) Check(ctx context.Context, req *authz.CheckRequest) (*authz.CheckResponse, error) {

	if s.throttler.Throttle(loadshedding.RequestInfo{PredictedCost: 1.0}) {
		return nil, grpc.Errorf(codes.Unavailable, "Envoy Server is currently overloaded. Please try again.")
	}

	envoyProtoBag := attribute.AuthzProtoBag(req)
	if s.cache != nil {
		if value, ok := s.cache.Get(envoyProtoBag); ok {
			var resp *authz.CheckResponse
			cacheStatus := rpc.Status{
				Code:    value.StatusCode,
				Message: value.StatusMessage,
			}
			if status.IsOK(cacheStatus) {
				lg.Debug("ExtAuthz.Check approved")
				resp = &authz.CheckResponse{
					Status: &rpcstatus.Status{
						Code:    value.StatusCode,
						Message: value.StatusMessage,
					},
					HttpResponse: &authz.CheckResponse_OkResponse{
						OkResponse: &authz.OkHttpResponse{},
					},
				}
			} else {
				lg.Debugf("ExtAuthz.Check denied: %v", value.StatusCode)
				resp = &authz.CheckResponse{
					Status: &rpcstatus.Status{
						Code:    value.StatusCode,
						Message: value.StatusMessage,
					},
					HttpResponse: &authz.CheckResponse_DeniedResponse{
						DeniedResponse: &authz.DeniedHttpResponse{},
					},
				}

			}

			lg.Debugf("ExtAuthz.Check() status from cache: %v", resp.Status)

			return resp, nil

		}
	}
	envoyCheckBag := attr.GetMutableBag(envoyProtoBag)
	resp, err := s.checkEnvoy(ctx, envoyProtoBag, envoyCheckBag)

	envoyProtoBag.Done()
	envoyCheckBag.Done()
	return resp, err
}

func (s *GrpcServerEnvoy) checkEnvoy(ctx context.Context, protoBag *attribute.EnvoyProtoBag,
	checkBag *attr.MutableBag) (*authz.CheckResponse, error) {

	if err := s.dispatcher.Preprocess(ctx, protoBag, checkBag); err != nil {
		err = fmt.Errorf("preprocessing attributes failed: %v", err)
		lg.Errora("ExtAuthz.Check failed: ", err.Error())
		return nil, grpc.Errorf(codes.Internal, err.Error())
	}

	lg.Debuga("ExtAuthz.Check Attribute Bag: \n", checkBag)
	lg.Debug("Dispatching ExtAuthz.Check")

	cr, err := s.dispatcher.Check(ctx, checkBag)
	if err != nil {
		err = fmt.Errorf("performing check operation failed: %v", err)
		lg.Errora("ExtAuthz.Check failed: ", err.Error())
		return nil, grpc.Errorf(codes.Internal, err.Error())
	}
	var resp *authz.CheckResponse
	if status.IsOK(cr.Status) {
		lg.Debug("ExtAuthz.Check approved")
		resp = &authz.CheckResponse{
			Status: &rpcstatus.Status{
				Code:    cr.Status.Code,
				Message: cr.Status.Message,
			},
			HttpResponse: &authz.CheckResponse_OkResponse{
				OkResponse: &authz.OkHttpResponse{},
			},
		}
	} else {
		lg.Debugf("ExtAuthz.Check denied: %v", cr.Status)
		resp = &authz.CheckResponse{
			Status: &rpcstatus.Status{
				Code:    cr.Status.Code,
				Message: cr.Status.Message,
			},
			HttpResponse: &authz.CheckResponse_DeniedResponse{
				DeniedResponse: &authz.DeniedHttpResponse{},
			},
		}

	}

	if s.cache != nil {
		// keep this for later...
		s.cache.Set(protoBag, checkcache.Value{
			StatusCode:           resp.Status.Code,
			StatusMessage:        cr.Status.Message,
			Expiration:           time.Now().Add(cr.ValidDuration),
			ValidUseCount:        cr.ValidUseCount,
			ReferencedAttributes: *protoBag.GetReferencedAttributes(s.globalDict, len(s.globalWordList)),
		})
	}

	return resp, nil
}

// Access log service should return empty response
// It is implemented to perform the same functionality as Mixer Report for OOP longevity
// It uses grpc Access Log Service API https://www.envoyproxy.io/docs/envoy/latest/api-v2/config/accesslog/v2/als.proto
func (s *GrpcServerEnvoy) StreamAccessLogs(srv accesslog.AccessLogService_StreamAccessLogsServer) error {
	for {
		ctx := context.Background()
		msg, err := srv.Recv()
		if err == io.EOF {
			return grpc.Error(codes.OK, "")
		}
		if err != nil {
			return err
		}

		var totalBags int
		if httpLogs := msg.GetHttpLogs(); httpLogs != nil {
			totalBags = len(httpLogs.GetLogEntry())
		} else if tcpLogs := msg.GetTcpLogs(); tcpLogs != nil {
			totalBags = len(tcpLogs.GetLogEntry())
		}

		reporter := s.dispatcher.GetReporter(ctx)
		var errors *multierror.Error
		var protoBag *attribute.EnvoyProtoBag
		var reportBag *attr.MutableBag

		for i := 0; i < totalBags; i++ {
			lg.Debugf("Dispatching Stream Access Logs Report %d out of %d", i+1, totalBags)

			protoBag = attribute.AccessLogProtoBag(msg, i)
			reportBag = attr.GetMutableBag(protoBag)
			if err := dispatchSingleReportEnvoy(ctx, s.dispatcher, reporter, protoBag, reportBag); err != nil {
				errors = multierror.Append(errors, err)
				continue
			}
			reportBag.Done()
			protoBag.Done()
		}
		if err := reporter.Flush(); err != nil {
			errors = multierror.Append(errors, err)
		}
		reporter.Done()

		if errors != nil {
			lg.Errora("Stream Access Log failed: ", errors.Error())
		}
	}
}

func dispatchSingleReportEnvoy(ctx context.Context, preprocessor dispatcher.Dispatcher, reporter dispatcher.Reporter,
	attributesBag *attribute.EnvoyProtoBag, reportBag *attr.MutableBag) error {

	lg.Debug("Dispatching Preprocess")

	if err := preprocessor.Preprocess(ctx, attributesBag, reportBag); err != nil {
		return fmt.Errorf("preprocessing attributes failed: %v", err)
	}
	if destinationNamespace, ok := reportBag.Get("destination.namespace"); ok {
		attributesBag.AddNamespaceDependentAttributes(destinationNamespace.(string))
	}

	lg.Debug("Dispatching to main adapters after running preprocessors")
	lg.Debuga("Attribute Bag: \n", reportBag)

	return reporter.Report(reportBag)
}
